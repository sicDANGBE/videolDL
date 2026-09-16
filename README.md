# videodl

Utilitaire Go de téléchargement HTTP(S), HLS et DASH avec une file persistante,
un suivi dans le terminal et un service en arrière-plan. Pas de dépendance Go externe.

## Première utilisation

Depuis ce dossier, avec l'exécutable fourni ou après compilation :

```sh
go build -trimpath -o videodl ./cmd/videodl
./videodl setup --destination "$HOME/Videos/videodl"
./videodl doctor
./videodl add --name video.mp4 'https://example.com/video.mp4'
./videodl worker
./videodl watch --once
```

L'URL est un exemple : fournissez une URL directe de média ou de playlist.
Les options doivent précéder l'URL ou l'identifiant.

`setup` crée la configuration complète, un guide, un manuel et les scripts de
complétion dans `~/.config/videodl/` (ou `$XDG_CONFIG_HOME/videodl/`). Il prépare
également les dossiers de données. Sans option, il conserve les réglages existants. `setup --destination DIR` change
uniquement la destination, même si la configuration existe déjà. Aucun téléchargement
ni service ne démarre. À la création sans destination explicite : `~/Downloads/videodl`.
L’aide générale affiche le profil effectif ; `VIDEODL_DESTINATION` reste prioritaire
sur la valeur du fichier. Les tâches déjà ajoutées gardent leur chemin de sortie.

Voir [config/README.md](config/README.md) pour les profils et le premier lancement.

## Aide et consultation

```sh
./videodl help
./videodl help worker
./videodl man
./videodl config path
./videodl config show
./videodl config get destination
./videodl config set concurrency 2
./videodl config edit
./videodl list
./videodl status IDENTIFIANT
./videodl watch --once
```

`list`, `status` et l'autocomplétion lisent la file sans la modifier. `doctor`
contrôle les chemins et FFmpeg sans réseau ni écriture. `config show` masque les
adresses de webhook et les commandes de notification ; `config get CLE` est une
consultation explicite de la valeur. La configuration JSON est écrite atomiquement
avec des permissions privées.

## Installation et autocomplétion

```sh
./bin/install.sh
```

Installe le programme sous `~/.local/bin`, le manuel sous `~/.local/share/man/man1`
et les scripts sous `~/.local/share`. Un préfixe absolu alternatif peut être passé
à l'installateur. Aucun fichier de démarrage du shell n'est modifié.

Bash, lorsque `videodl` est sur votre PATH :

```bash
source <(videodl completion bash)
```

Zsh : `source <(videodl completion zsh)` après `compinit`.
Fish : `videodl completion fish | source`.
Pour l'activation permanente, voir le guide généré par `setup`.

Manuel : `man videodl` après installation si le préfixe figure dans le chemin
recherché par man ; sinon `man -l internal/cli/assets/videodl.1`.
`./videodl man` fonctionne sans installer man.

## Fiabilité et réglages

- La surveillance continue après l'échec d'une tâche ; les erreurs de stockage
  restent signalées. Un verrou de file occupé est attendu brièvement.
- La progression est sauvegardée au plus une fois par seconde pendant le transfert,
  et à la fin. Un arrêt brutal peut perdre la dernière seconde de progression affichée.
- Les fichiers finaux ne sont jamais écrasés, même en cas de concurrence.
  Deux tâches actives ne peuvent pas réserver le même chemin de sortie.
- Reprise HTTP avec ETag fort et vérification de Content-Range. Un serveur ignorant
  Range ou renvoyant une nouvelle version provoque un redémarrage depuis le début.
  Une réponse Range incohérente est refusée et le partiel abandonné.
- Nouvelles tentatives bornées sur erreurs transitoires ; les erreurs 401/403/404
  ne sont pas réessayées automatiquement. Retry-After est respecté jusqu'à 5 minutes.
- Les lectures réseau bloquées sont interrompues par `idle_timeout`.
- Sélection HLS avec `--max-height 720` ou `1080`. Zéro conserve le meilleur débit.
  FFmpeg assemble les pistes audio séparées et remuxe MPEG-TS en MP4/MKV sans réencodage.

| Clé de configuration | Défaut | Option |
|---|---|---|
| `retries` | 3 | `--retries 3` (0 à 10) |
| `resume` | true | `--resume=false` |
| `max_height` | 0 | `--max-height 1080` (0 à 8640) |
| `idle_timeout` | 60s | `--idle-timeout 60s` |
| `timeout` | 30s | `--timeout 30s` |
| `concurrency` | 1 | `--concurrency 2` (1 à 8) |
| `min_free_space` | 2GiB | `config set min_free_space 2GiB` |

Les anciennes configurations restent acceptées. Les variables `VIDEODL_RETRIES`,
`VIDEODL_RESUME`, `VIDEODL_MAX_HEIGHT`, `VIDEODL_IDLE_TIMEOUT` et
`VIDEODL_MIN_FREE_SPACE` sont aussi disponibles.
Les options de transfert explicites de `add` sont enregistrées avec la tâche ;
les options explicites de `worker` les remplacent pour cette exécution. Sinon,
la configuration effective du worker est utilisée.

Chaque destination peut contenir `.videodl/` avec les fichiers partiels HTTP,
leurs validateurs et des petits fichiers de verrou persistants. Les métadonnées
utilisent une empreinte de l'URL, sans la stocker en clair. Les partiels récupérables
sont conservés après interruption/annulation et réutilisables par `retry`.
HLS et FFmpeg ne reprennent pas au dernier octet. La reprise n'est pas garantie
sans ETag fort. Les transferts natifs réessaient les requêtes ; les nouvelles
tentatives ne pilotent pas un processus FFmpeg en échec.

## Supervision et espace disque

Un seul superviseur (`worker` ou daemon) peut traiter une même file. `concurrency`
est la limite de téléchargements simultanés, de 1 à 8. Avec la valeur 8, neuf tâches
produisent huit transferts actifs et une tâche en attente. Aucun processus
supplémentaire n’est créé par tranche de huit tâches.

`worker` traite les tâches en attente au démarrage, puis quitte. `worker --watch`
et le daemon acceptent les nouveaux ajouts pendant les transferts en cours. Une
place libérée est réutilisée immédiatement. Une lecture commune de la file chaque
seconde détecte les nouveaux ajouts et les annulations (délai habituel : une seconde).
La progression reste écrite au plus une fois par seconde et par tâche, puis à la fin.

`min_free_space` réserve une marge de **2 Gio** par défaut. Les tailles HTTP connues
sont additionnées pour les transferts du même superviseur sur le même système de
fichiers. Les tailles inconnues utilisent une réserve renouvelable de 64 Mio par
transfert. Le contrôle se répète pendant les écritures natives et toutes les 250 ms
pour FFmpeg. En cas d’espace insuffisant, la tâche passe en échec ; libérez de
l’espace, puis utilisez `retry ID`. Les partiels HTTP récupérables sont conservés.

Ce contrôle est préventif, pas un quota : les autres processus et les écritures de
FFmpeg entre deux vérifications peuvent réduire la marge. Des superviseurs de files
différentes ne partagent pas leurs réservations. `0` supprime la marge minimale,
mais conserve les vérifications de capacité. Les unités acceptées sont des entiers
en octets, KiB, MiB, GiB ou TiB. `doctor` affiche l’espace disponible et la marge.

Après un changement de réglage, un service actif doit être redémarré avec
`videodl daemon restart`. `daemon stop` attend sa sortie avant d’annoncer l’arrêt.

## Arrière-plan

```sh
./videodl daemon start
./videodl daemon status
./videodl daemon logs
./videodl daemon stop
```

Pour lancer le service automatiquement aux prochains ajouts :
`./videodl config set auto_start_worker true`.

**Mise à jour :** arrêtez l'ancien daemon avant de remplacer son exécutable.
La nouvelle version lit les anciennes files. Une file réécrite avec les options
par tâche ne doit pas être ouverte avec l'ancien exécutable (schéma strict).

## Limites

Go 1.23+ pour compiler ; Linux pour le daemon et les verrous. FFmpeg est requis
pour DASH, HLS avec audio séparé/byte ranges/discontinuités et le remuxage TS vers
MP4/MKV. Le moteur HLS intégré traite un instantané de playlist, pas une capture
de direct continue. `max_height` s'applique à HLS intégré, pas à DASH ni au mode
`--ffmpeg` forcé. Le contrôle de chiffrement existant est inchangé.
Le programme ne fournit pas d'extraction de page web ni de connexion à un site.

## Validation

```sh
go test -race -shuffle=on -count=1 ./...
go vet ./...
go build -trimpath -o videodl ./cmd/videodl
```

Le test d'intégration média utilise FFmpeg/ffprobe si disponibles ; aucun site
externe n'est contacté. Guide détaillé : [docs/INSTALLATION.md](docs/INSTALLATION.md).
