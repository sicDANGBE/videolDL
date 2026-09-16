// Package installer updates user-local files and restarts only the daemons that
// were using this installation. It never rewrites configuration or queue data.
package installer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

type Options struct {
	Prefix, Binary, Manual string
	Out                    io.Writer
	replace                func(string, string) error
}

func Install(ctx context.Context, options Options) (err error) {
	if !filepath.IsAbs(options.Prefix) {
		return fmt.Errorf("PREFIX doit être un chemin absolu")
	}
	if options.Out == nil {
		options.Out = io.Discard
	}
	if options.replace == nil {
		options.replace = os.Rename
	}
	unlock, err := lockInstall(options.Prefix)
	if err != nil {
		return err
	}
	defer unlock()
	prefix, err := filepath.EvalSymlinks(options.Prefix)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(prefix, "bin"), 0755); err != nil {
		return err
	}
	binDir, err := filepath.EvalSymlinks(filepath.Join(prefix, "bin"))
	if err != nil {
		return err
	}
	executable := filepath.Join(binDir, "videodl")
	_, statErr := os.Lstat(executable)
	updating := statErr == nil
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	}
	var files []stagedFile
	preserveBackups := false
	defer func() {
		if !preserveBackups {
			for _, f := range files {
				f.cleanup()
			}
		}
	}()
	add := func(target string, data []byte, mode os.FileMode) error {
		f, err := stage(target, bytes.NewReader(data), mode)
		if err == nil {
			files = append(files, f)
		}
		return err
	}
	binary, err := os.ReadFile(options.Binary)
	if err != nil {
		return err
	}
	if err := add(executable, binary, 0755); err != nil {
		return err
	}
	manual, err := os.ReadFile(options.Manual)
	if err != nil {
		return err
	}
	if err := add(filepath.Join(prefix, "share/man/man1/videodl.1"), manual, 0644); err != nil {
		return err
	}
	for _, shell := range []struct{ name, path string }{{"bash", "bash-completion/completions/videodl"}, {"zsh", "zsh/site-functions/_videodl"}, {"fish", "fish/vendor_completions.d/videodl.fish"}} {
		command := exec.CommandContext(ctx, options.Binary, "completion", shell.name)
		script, err := command.Output()
		if err != nil {
			return fmt.Errorf("préparer la complétion %s : %w", shell.name, err)
		}
		if err := add(filepath.Join(prefix, "share", shell.path), script, 0644); err != nil {
			return err
		}
	}
	services, err := servicesAt(executable)
	if err != nil {
		return err
	}
	if len(services) > 0 && !updating {
		return fmt.Errorf("un service utilise un exécutable supprimé sans copie installée ; restauration manuelle requise")
	}
	mode := "Installation"
	if updating {
		mode = "Mise à jour"
	}
	fmt.Fprintf(options.Out, "%s préparée : %s\nServices actifs à relancer : %d\n", mode, executable, len(services))
	if len(services) > 0 {
		fmt.Fprintln(options.Out, "Transferts interrompus : HTTP reprend si possible ; HLS/FFmpeg recommence la tâche.")
	}
	var started []service
	recoveryNeeded := false
	// On a handled failure or signal, restore package files before relaunching
	// the previous version. Queue progress is left intact, never rolled back.
	defer func() {
		if err == nil || !recoveryNeeded {
			return
		}
		recoveryCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		for _, s := range started {
			if stopErr := stopService(recoveryCtx, s); stopErr != nil {
				preserveBackups = true
				err = errors.Join(err, stopErr, fmt.Errorf("restauration suspendue ; sauvegardes conservées sous %s", prefix))
				return
			}
		}
		for i := len(files) - 1; i >= 0; i-- {
			if restoreErr := files[i].restore(); restoreErr != nil {
				preserveBackups = true
				err = errors.Join(err, restoreErr, fmt.Errorf("sauvegardes conservées sous %s", prefix))
				return
			}
		}
		for _, s := range services {
			if !s.alive() {
				if _, restartErr := startService(recoveryCtx, executable, s); restartErr != nil {
					err = errors.Join(err, fmt.Errorf("ancienne version restaurée, service à vérifier : %w", restartErr))
					return
				}
			}
		}
		fmt.Fprintln(options.Out, "Changements annulés ; fichiers précédents restaurés et services arrêtés pour la mise à jour relancés.")
	}()
	recoveryNeeded = true
	for _, s := range services {
		fmt.Fprintf(options.Out, "Arrêt propre du service PID %d…\n", s.pid)
		stopCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		stopErr := stopService(stopCtx, s)
		cancel()
		if stopErr != nil {
			return stopErr
		}
	}
	// Refuse a process started during preparation instead of replacing its file.
	remaining, err := servicesAt(executable)
	if err != nil {
		return err
	}
	if len(remaining) > 0 {
		return fmt.Errorf("un service a démarré pendant la mise à jour ; réessayer")
	}
	for i := range files {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := options.replace(files[i].prepared, files[i].target); err != nil {
			return fmt.Errorf("installer %s : %w", files[i].target, err)
		}
		files[i].applied = true
	}
	for _, s := range services {
		startCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		next, startErr := startService(startCtx, executable, s)
		cancel()
		if next.pid != 0 {
			started = append(started, next)
		}
		if startErr != nil {
			return startErr
		}
		fmt.Fprintf(options.Out, "Service relancé et file prise en charge : PID %d\n", next.pid)
	}
	recoveryNeeded = false
	fmt.Fprintf(options.Out, "%s terminée : %s\nConfiguration conservée ; file et fichiers finaux préservés.\n", mode, executable)
	if updating {
		fmt.Fprintln(options.Out, "Vérification : videodl doctor. Inutile de relancer setup.")
	} else {
		fmt.Fprintf(options.Out, "Ajoutez %s au PATH si nécessaire.\nSi aucune configuration n’existe : videodl setup --destination DIR\nVérification : videodl doctor\n", binDir)
	}
	if len(services) == 0 {
		fmt.Fprintln(options.Out, "Aucun service actif à relancer ; aucun service démarré.")
	}
	fmt.Fprintf(options.Out, "Manuel : man -l %s\nBash : source <(videodl completion bash)\n", filepath.Join(prefix, "share/man/man1/videodl.1"))
	return nil
}
