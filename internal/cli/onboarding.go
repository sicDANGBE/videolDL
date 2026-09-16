package cli

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"video-downloader/internal/config"
)

//go:embed assets/quickstart.md
var quickstart string

//go:embed assets/videodl.1
var manual string

func topHelp(options Options) error {
	out := options.Out
	fmt.Fprintln(out, "videodl — téléchargement de vidéos HTTP/HLS et file persistante")
	wd, err := configWorkingDir(options)
	var loaded config.Config
	if err == nil {
		loaded, err = config.Load(config.LoadOptions{HomeDir: options.HomeDir, WorkingDir: wd, Env: options.Env})
	}
	if err != nil {
		fmt.Fprintln(out, "\nConfiguration indisponible : lancez videodl doctor pour le diagnostic.")
	} else if _, statErr := os.Stat(loaded.ConfigPath); statErr == nil {
		fmt.Fprintf(out, "\nConfiguration existante : %s\nDestination effective : %s\nTéléchargements simultanés : %d (maximum 8)\nMarge disque : %s\nDémarrage automatique : %t\n", loaded.ConfigPath, loaded.Destination, loaded.Concurrency, config.FormatSpace(loaded.MinFreeSpace), loaded.AutoStartWorker)
		fmt.Fprintln(out, "\nChanger de destination : videodl config set destination DIR\nVérifier les réglages : videodl doctor")
	} else if os.IsNotExist(statErr) {
		fmt.Fprintf(out, "\nAucun fichier de configuration : valeurs par défaut et environnement.\nDestination effective : %s\nPremière utilisation :\n  videodl setup --destination DIR\n  videodl doctor\n", loaded.Destination)
	} else {
		fmt.Fprintln(out, "\nFichier de configuration inaccessible : lancez videodl doctor.")
	}
	fmt.Fprintln(out, "\nAjouter : videodl add --name video.mp4 URL\nSuivre : videodl watch --once")
	if err == nil && loaded.AutoStartWorker {
		fmt.Fprintln(out, "Le service démarre automatiquement à l’ajout ; état : videodl daemon status")
	} else {
		fmt.Fprintln(out, "Traiter la file : videodl worker (ou daemon start pour surveiller les ajouts)")
	}
	_, err = fmt.Fprintln(out, `
Commandes :
  setup                 Préparer les fichiers ; --destination DIR change la destination
  doctor                Vérifier la configuration et les outils disponibles
  add                   Ajouter une URL à la file
  worker [--watch]       Traiter la file initiale ou surveiller les nouveaux ajouts
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
Les options précèdent l'URL ou l'identifiant. Aucun fichier vidéo existant n'est écrasé.
Les réglages s’appliquent aux prochains processus ; un service actif doit être redémarré.`)
	return err
}

func runSetup(options Options, args []string) error {
	set := flag.NewFlagSet("videodl setup", flag.ContinueOnError)
	set.SetOutput(options.Out)
	var path, destination string
	set.StringVar(&path, "config", "", "chemin du fichier de configuration")
	set.StringVar(&destination, "destination", "", "créer ou changer la destination des vidéos (autres réglages préservés)")
	set.Usage = func() {
		fmt.Fprintln(options.Out, "Usage: videodl setup [--config FILE] [--destination DIR]\nPrépare les fichiers manquants. Sans option, préserve les réglages ; --destination modifie uniquement cette valeur.")
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
	_, statErr := os.Stat(selected)
	if statErr != nil && !os.IsNotExist(statErr) {
		return statErr
	}
	if destination != "" {
		resolved, err := config.ResolveDestination(destination, wd)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(resolved, 0o700); err != nil {
			return fmt.Errorf("créer la destination : %w", err)
		}
		loaded.Destination = resolved
	}
	if os.IsNotExist(statErr) {
		if err := writePersistedConfig(selected, fromConfig(loaded)); err != nil {
			return err
		}
	} else if destination != "" {
		// Patch only the explicitly requested field; environment overrides must not
		// become permanent settings as a side effect of setup.
		data, err := os.ReadFile(selected)
		if err != nil {
			return err
		}
		var stored map[string]json.RawMessage
		if err := json.Unmarshal(data, &stored); err != nil {
			return err
		}
		if stored == nil {
			stored = make(map[string]json.RawMessage)
		}
		stored["destination"], err = json.Marshal(loaded.Destination)
		if err != nil {
			return err
		}
		data, err = json.MarshalIndent(stored, "", "  ")
		if err != nil {
			return err
		}
		if err := writeConfigAtomic(selected, append(data, '\n')); err != nil {
			return err
		}
	}
	loaded, err = config.Load(config.LoadOptions{ConfigPath: selected, HomeDir: options.HomeDir, WorkingDir: wd, Env: options.Env})
	if err != nil {
		return err
	}
	if err := writeSetupFiles(selected, loaded); err != nil {
		return err
	}
	_, err = fmt.Fprintf(options.Out, "Configuration prête : %s\nDestination effective : %s\nGuide : %s\nSuite : videodl doctor, puis videodl add --name video.mp4 URL\n", selected, loaded.Destination, filepath.Join(filepath.Dir(selected), "README.md"))
	if destination != "" && envValue(options.Env, "VIDEODL_DESTINATION") != "" {
		fmt.Fprintln(options.Out, "VIDEODL_DESTINATION est prioritaire sur la destination enregistrée.")
	}
	if loaded.AutoStartWorker {
		fmt.Fprintln(options.Out, "Démarrage automatique activé ; suivi : videodl watch --once")
	} else {
		fmt.Fprintln(options.Out, "Traiter la file : videodl worker ; suivi : videodl watch --once")
	}
	fmt.Fprintln(options.Out, "Si un service est actif, appliquez les réglages avec videodl daemon restart.")
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
	fmt.Fprintf(options.Out, "Concurrence : %d / 8 ; marge disque : %s\n", loaded.Concurrency, config.FormatSpace(loaded.MinFreeSpace))
	var disk syscall.Statfs_t
	if err := syscall.Statfs(loaded.Destination, &disk); err == nil {
		free := int64(disk.Bavail) * int64(disk.Bsize)
		fmt.Fprintf(options.Out, "Espace disponible : %.2f Gio\n", float64(free)/(1<<30))
		if free < loaded.MinFreeSpace {
			return fmt.Errorf("espace disponible inférieur à min_free_space")
		}
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
		return topHelp(options)
	}
	if len(args) > 1 || !isCommand(args[0]) || strings.HasPrefix(args[0], "__") || args[0] == "help" {
		return fmt.Errorf("usage: videodl help [COMMANDE]")
	}
	options.Args = []string{args[0], "--help"}
	return Run(ctx, options)
}
