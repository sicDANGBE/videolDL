package cli

import (
	"flag"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"
)

var optionShortNames = map[string]string{
	"config": "c", "state": "s", "log": "l", "destination": "d", "name": "n", "output": "o",
	"concurrency": "j", "timeout": "t", "ffmpeg": "f", "ffmpeg-path": "F", "webhook": "W",
	"json": "J", "watch": "w", "once": "1", "interval": "i", "retries": "r", "resume": "R",
	"max-height": "H", "idle-timeout": "T", "editor": "e", "help": "h",
}

func canonicalOption(name string) string {
	for long, short := range optionShortNames {
		if name == short {
			return long
		}
	}
	return name
}

func commandOptionNames(command string) []string {
	const transfer = "retries resume max-height idle-timeout ffmpeg"
	var names string
	switch command {
	case "":
		names = "config output timeout ffmpeg-path " + transfer
	case "add":
		names = "config state log destination name output webhook json " + transfer
	case "worker":
		names = "config state log concurrency timeout ffmpeg-path webhook watch " + transfer
	case "__daemon-worker":
		names = "config state log concurrency timeout ffmpeg-path webhook " + transfer
	case "list", "status":
		names = "config state json"
	case "retry", "cancel":
		names = "config state log webhook json"
	case "watch":
		names = "config state json once interval"
	case "setup":
		names = "config destination"
	case "doctor":
		names = "config"
	case "config show":
		names = "config json"
	case "config edit":
		names = "config editor"
	case "config init", "config path", "config get", "config set":
		names = "config"
	case "daemon start", "daemon stop", "daemon restart", "daemon status", "daemon logs":
		names = "config"
	}
	return strings.Fields(names + " help")
}

// Parsing, help and shell completion all use these same flag definitions.
// Constructing a FlagSet never loads settings, opens the queue or creates files.
func commandFlagSet(command string, values *commandFlags, out io.Writer) *flag.FlagSet {
	set := flag.NewFlagSet(strings.TrimSpace("videodl "+command), flag.ContinueOnError)
	set.SetOutput(out)
	for _, name := range commandOptionNames(command) {
		switch name {
		case "config":
			set.StringVar(&values.configPath, name, "", "fichier de configuration")
		case "state":
			set.StringVar(&values.statePath, name, "", "fichier de la file persistante")
		case "log":
			set.StringVar(&values.logPath, name, "", "répertoire des journaux")
		case "destination":
			set.StringVar(&values.destination, name, "", "répertoire de destination des nouveaux ajouts")
		case "name":
			set.StringVar(&values.name, name, "", "nom du fichier de sortie, sans répertoire")
		case "output":
			set.StringVar(&values.output, name, "", "fichier de sortie (alias de --name pour add)")
		case "concurrency":
			set.IntVar(&values.concurrency, name, 0, "limite de téléchargements simultanés (1 à 8 ; configuration si omise)")
		case "timeout":
			set.DurationVar(&values.timeout, name, 30*time.Second, "délai réseau par connexion et réponse HTTP")
		case "ffmpeg":
			set.BoolVar(&values.ffmpeg, name, false, "forcer le traitement par FFmpeg")
		case "ffmpeg-path":
			set.StringVar(&values.ffmpegPath, name, "", "chemin de l’exécutable FFmpeg")
		case "webhook":
			set.StringVar(&values.webhookURL, name, "", "URL webhook d’observabilité")
		case "json":
			set.BoolVar(&values.json, name, false, "émettre du JSON")
		case "watch":
			set.BoolVar(&values.watch, name, false, "surveiller les nouveaux ajouts en continu")
		case "once":
			set.BoolVar(&values.once, name, false, "afficher l’état une fois puis quitter")
		case "interval":
			set.DurationVar(&values.interval, name, 2*time.Second, "intervalle de rafraîchissement")
		case "retries":
			set.IntVar(&values.retries, name, 3, "nouvelles tentatives réseau (0 à 10)")
		case "resume":
			set.BoolVar(&values.resume, name, true, "reprendre HTTP si possible ; --resume=false pour désactiver")
		case "max-height":
			set.IntVar(&values.maxHeight, name, 0, "hauteur HLS maximale (0 = meilleur débit)")
		case "idle-timeout":
			set.DurationVar(&values.idleTimeout, name, time.Minute, "délai maximal sans données HTTP")
		case "editor":
			set.StringVar(&values.editor, name, "", "éditeur pour cette invocation")
		case "help":
			set.BoolVar(&values.help, name, false, "afficher cette aide")
		}
		original := set.Lookup(name)
		if short := optionShortNames[name]; short != "" {
			set.Var(original.Value, short, original.Usage)
		}
	}
	set.Usage = func() {
		fmt.Fprintln(out, commandUsage(command)+"\n\nOptions :")
		printOptions(set, out)
		fmt.Fprintln(out, "Les options précèdent les arguments. Valeur séparée par un espace ou =.\nBooléens : =true ou =false ; options courtes séparées (pas de regroupement).")
	}
	return set
}
func commandUsage(command string) string {
	switch command {
	case "":
		return "Usage: videodl [options] --output FICHIER URL"
	case "add":
		return "Usage: videodl add [options] [NOM] URL\n       videodl add [options] --name NOM URL"
	case "setup":
		return "Usage: videodl setup [options]\nSans option : réglages préservés. -d/--destination modifie uniquement la destination."
	case "status", "retry", "cancel":
		return "Usage: videodl " + command + " [options] ID"
	case "config get":
		return "Usage: videodl config get [options] CLE"
	case "config set":
		return "Usage: videodl config set [options] CLE VALEUR"
	default:
		return "Usage: videodl " + command + " [options]"
	}
}
func booleanOption(value *flag.Flag) bool {
	b, ok := value.Value.(interface{ IsBoolFlag() bool })
	return ok && b.IsBoolFlag()
}
func printOptions(set *flag.FlagSet, out io.Writer) {
	table := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	set.VisitAll(func(value *flag.Flag) {
		if canonicalOption(value.Name) != value.Name {
			return
		}
		spelling := "--" + value.Name
		if short := optionShortNames[value.Name]; short != "" {
			spelling = "-" + short + ", " + spelling
		}
		if !booleanOption(value) {
			meta := "VALEUR"
			switch value.Name {
			case "name":
				meta = "NOM"
			case "config", "state", "ffmpeg-path", "output":
				meta = "FICHIER"
			case "destination", "log":
				meta = "DOSSIER"
			case "timeout", "interval", "idle-timeout":
				meta = "DURÉE"
			case "concurrency", "retries", "max-height":
				meta = "N"
			case "webhook":
				meta = "URL"
			}
			spelling += " " + meta
		}
		description := value.Usage
		if value.DefValue != "" && value.DefValue != "0" && value.DefValue != "false" {
			description += " (défaut : " + value.DefValue + ")"
		}
		fmt.Fprintf(table, "  %s\t%s\n", spelling, description)
	})
	table.Flush()
}
