package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
)

type Options struct {
	Args       []string
	Out        io.Writer
	ErrOut     io.Writer
	Client     *http.Client
	HomeDir    string
	WorkingDir string
	Env        []string
}

func Run(ctx context.Context, options Options) error {
	if options.Out == nil {
		options.Out = io.Discard
	}
	if options.ErrOut == nil {
		options.ErrOut = os.Stderr
	}
	if len(options.Args) == 0 || (len(options.Args) == 1 && (options.Args[0] == "--help" || options.Args[0] == "-h")) {
		return topHelp(options)
	}
	if !isCommand(options.Args[0]) {
		return runLegacy(ctx, options)
	}
	switch options.Args[0] {
	case "setup":
		return runSetup(options, options.Args[1:])
	case "doctor":
		return runDoctor(options, options.Args[1:])
	case "help":
		return runHelp(ctx, options, options.Args[1:])
	case "man":
		if len(options.Args) > 1 && options.Args[1] != "--help" && options.Args[1] != "-h" {
			return fmt.Errorf("usage: videodl man")
		}
		_, err := fmt.Fprint(options.Out, quickstart)
		return err
	case "add":
		return runAdd(ctx, options, options.Args[1:])
	case "worker":
		return runWorker(ctx, options, options.Args[1:])
	case "list":
		return runList(ctx, options, options.Args[1:])
	case "status":
		return runStatus(ctx, options, options.Args[1:])
	case "watch":
		return runWatch(ctx, options, options.Args[1:])
	case "retry":
		return runTransition(ctx, options, options.Args[1:], true)
	case "cancel":
		return runTransition(ctx, options, options.Args[1:], false)
	case "config":
		return runConfigCommand(ctx, options, options.Args[1:])
	case "daemon":
		return runDaemonCommand(ctx, options, options.Args[1:])
	case "completion":
		return runCompletion(options, options.Args[1:])
	case "__complete":
		return runHiddenCompletion(ctx, options, options.Args[1:])
	case "__daemon-worker":
		return runDaemonWorker(ctx, options, options.Args[1:])
	default:
		return fmt.Errorf("unknown command %q", options.Args[0])
	}
}

func isCommand(value string) bool {
	switch value {
	case "setup", "doctor", "help", "man", "add", "worker", "list", "status", "retry", "cancel", "config", "daemon", "watch", "completion", "__complete", "__daemon-worker":
		return true
	default:
		return false
	}
}

func legacyHelp(out io.Writer) {
	flags := flag.NewFlagSet("videodl", flag.ContinueOnError)
	flags.SetOutput(out)
	flags.String("o", "", "chemin du fichier de sortie")
	flags.String("output", "", "chemin du fichier de sortie")
	flags.Bool("ffmpeg", false, "forcer le traitement par ffmpeg")
	flags.String("ffmpeg-path", "", "chemin de l'exécutable ffmpeg")
	flags.Duration("timeout", 0, "délai réseau par connexion et réponse HTTP")
	_, _ = fmt.Fprintln(out, "Queue commands: add, worker, list, status, watch, retry, cancel, config, daemon, completion")
}
