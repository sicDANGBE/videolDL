package cli

import (
	"context"
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"video-downloader/internal/config"
)

//go:embed assets/quickstart.md
var quickstart string

//go:embed assets/videodl.1
var manual string

func topHelp(out io.Writer) error {
	_, err := fmt.Fprintln(out, `videodl — téléchargement de vidéos HTTP/HLS et file persistante

Première utilisation :
  videodl setup --destination "$HOME/Videos/videodl"
  videodl doctor
  videodl add --name video.mp4 URL
  videodl worker
  videodl watch --once

Commandes :
  setup                 Préparer la configuration et le guide local
  doctor                Vérifier la configuration et les outils disponibles
  add                   Ajouter une URL à la file
  worker [--watch]      Télécharger, puis quitter ou surveiller la file
  list / status ID      Consulter les téléchargements
  watch [--once]        Tableau de suivi
  retry ID / cancel ID  Relancer ou annuler
  daemon                start, stop, restart, status, logs
  config                init, path, show, get, set, edit
  completion            bash, zsh, fish
  help [COMMANDE]        Aide générale ou détaillée
  man                   Guide intégré consultable sans installation

Téléchargement immédiat :
  videodl --output video.mp4 URL

Options de transfert : --config, --retries, --resume=false,
  --max-height, --idle-timeout, --timeout, --ffmpeg, --ffmpeg-path
Les options précèdent l'URL ou l'identifiant. Aucun fichier existant n'est écrasé.
Queue commands: add, worker, list, status, watch, retry, cancel, config, daemon, completion`)
	return err
}

func runSetup(options Options, args []string) error {
	set := flag.NewFlagSet("videodl setup", flag.ContinueOnError)
	set.SetOutput(options.Out)
	var path, destination string
	set.StringVar(&path, "config", "", "chemin du fichier de configuration")
	set.StringVar(&destination, "destination", "", "destination des vidéos, à la création uniquement")
	set.Usage = func() {
		fmt.Fprintln(options.Out, "Usage: videodl setup [--config FILE] [--destination DIR]\nPrépare les fichiers manquants et préserve la configuration existante.")
		set.PrintDefaults()
	}
	if err := set.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if set.NArg() != 0 {
		return fmt.Errorf("usage: videodl setup [options]")
	}
	wd, err := configWorkingDir(options)
	if err != nil {
		return err
	}
	selected, loaded, err := loadForConfigCommand(options, configCommandFlags{configPath: path}, wd, false)
	if err != nil {
		return err
	}
	if !filepath.IsAbs(selected) {
		selected = filepath.Join(wd, selected)
	}
	if _, err := os.Stat(selected); os.IsNotExist(err) {
		if destination != "" {
			loaded.Destination, err = config.ResolveDestination(destination, wd)
			if err != nil {
				return err
			}
		}
		if err := writePersistedConfig(selected, fromConfig(loaded)); err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else if destination != "" {
		resolved, err := config.ResolveDestination(destination, wd)
		if err != nil {
			return err
		}
		if resolved != loaded.Destination {
			return fmt.Errorf("configuration existante préservée ; utilisez videodl config set destination DIR pour la changer")
		}
	}
	if err := writeSetupFiles(selected, loaded); err != nil {
		return err
	}
	_, err = fmt.Fprintf(options.Out, "Configuration prête : %s\nDestination : %s\nGuide : %s\nSuite : videodl doctor, puis videodl add --name video.mp4 URL et videodl worker\n", selected, loaded.Destination, filepath.Join(filepath.Dir(selected), "README.md"))
	return err
}
func writeSetupFiles(path string, c config.Config) error {
	for _, directory := range []string{filepath.Dir(path), filepath.Dir(c.StatePath), c.LogPath, filepath.Dir(c.DaemonPIDPath), filepath.Dir(c.DaemonLogPath), c.Destination} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return err
		}
	}
	files := map[string]string{"README.md": quickstart, "videodl.1": manual}
	for _, shell := range completionShells {
		script, err := completionScript(shell)
		if err != nil {
			return err
		}
		files[filepath.Join("completions", "videodl."+shell)] = script
	}
	for relative, data := range files {
		target := filepath.Join(filepath.Dir(path), relative)
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		file, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if os.IsExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		_, writeErr := io.WriteString(file, data)
		closeErr := file.Close()
		if err := errors.Join(writeErr, closeErr); err != nil {
			return err
		}
	}
	return nil
}
func runDoctor(options Options, args []string) error {
	set := flag.NewFlagSet("videodl doctor", flag.ContinueOnError)
	set.SetOutput(options.Out)
	var path string
	set.StringVar(&path, "config", "", "fichier de configuration")
	if err := set.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if set.NArg() != 0 {
		return fmt.Errorf("usage: videodl doctor [--config FILE]")
	}
	wd, err := configWorkingDir(options)
	if err != nil {
		return err
	}
	loaded, err := config.Load(config.LoadOptions{ConfigPath: path, HomeDir: options.HomeDir, WorkingDir: wd, Env: options.Env})
	if err != nil {
		return err
	}
	fmt.Fprintln(options.Out, "Configuration : valide")
	for _, item := range []struct {
		name, path string
		directory  bool
	}{{"Fichier", loaded.ConfigPath, false}, {"Destination", loaded.Destination, true}, {"File", loaded.StatePath, false}, {"Journaux", loaded.LogPath, true}} {
		info, statErr := os.Stat(item.path)
		state := "présent"
		if os.IsNotExist(statErr) {
			state = "absent — créé par setup ou au premier usage"
		} else if statErr != nil {
			return statErr
		} else if info.IsDir() != item.directory {
			return fmt.Errorf("type de chemin incorrect : %s", item.path)
		}
		fmt.Fprintf(options.Out, "%s : %s (%s)\n", item.name, item.path, state)
	}
	ffmpeg := loaded.FFmpegPath
	if ffmpeg == "" {
		ffmpeg = "ffmpeg"
	}
	executable, err := exec.LookPath(ffmpeg)
	if err != nil {
		fmt.Fprintln(options.Out, "FFmpeg : absent (requis pour DASH et assemblage HLS avancé)")
		if loaded.FFmpeg {
			return fmt.Errorf("FFmpeg configuré mais introuvable")
		}
	} else {
		fmt.Fprintf(options.Out, "FFmpeg : %s\n", executable)
	}
	fmt.Fprintln(options.Out, "Diagnostic en lecture seule ; réseau et écriture non testés.")
	return nil
}
func runHelp(ctx context.Context, options Options, args []string) error {
	if len(args) == 0 {
		return topHelp(options.Out)
	}
	if len(args) > 1 || !isCommand(args[0]) || strings.HasPrefix(args[0], "__") || args[0] == "help" {
		return fmt.Errorf("usage: videodl help [COMMANDE]")
	}
	options.Args = []string{args[0], "--help"}
	return Run(ctx, options)
}
