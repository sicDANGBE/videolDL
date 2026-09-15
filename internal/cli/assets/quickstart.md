# videodl — première utilisation

Ce dossier contient votre `config.json`, ce guide, le manuel `videodl.1` et
les scripts d'autocomplétion `completions/` pour Bash, Zsh et Fish.
`videodl setup` complète les fichiers manquants sans remplacer vos réglages.
Aucun service ne démarre pendant la configuration.

## Démarrage

    videodl setup --destination "$HOME/Videos/videodl"
    videodl doctor
    videodl add --name exemple.mp4 'https://example.com/video.mp4'
    videodl worker
    videodl watch --once

Les URL ci-dessus sont des exemples. Utilisez une URL directe de fichier ou de flux.
Les options se placent AVANT l'URL ou l'identifiant de tâche.

Pour travailler en arrière-plan :

    videodl daemon start
    videodl daemon status
    videodl daemon stop

Pour les prochaines tâches ajoutées, le démarrage peut être automatisé :

    videodl config set auto_start_worker true

## Consulter et régler

    videodl help
    videodl help worker
    videodl config path
    videodl config show
    videodl config get destination
    videodl config set concurrency 2
    videodl config set retries 3
    videodl config set resume true
    videodl config set max_height 1080
    videodl config set idle_timeout 60s
    videodl config edit
    videodl list
    videodl status IDENTIFIANT
    videodl retry IDENTIFIANT
    videodl cancel IDENTIFIANT
    videodl man

`config show` présente les réglages effectifs (fichier + environnement).
Les adresses de webhook y sont masquées ; `config get webhook_url` permet une
consultation explicite. La configuration reste un fichier privé (permissions 600).

## Autocomplétion

Après installation du programme dans votre PATH :

Bash, pour le terminal actuel :

    source <(videodl completion bash)

Zsh, après `autoload -Uz compinit; compinit` :

    source <(videodl completion zsh)

Fish :

    videodl completion fish | source

Pour une activation persistante, `bin/install.sh` installe les scripts dans les
emplacements usuels sous ~/.local/share. Pour Bash sans chargeur automatique,
ajoutez la commande `source` ci-dessus à ~/.bashrc. Pour Zsh, ajoutez-la à ~/.zshrc
après compinit. Pour Fish, utilisez `videodl completion fish >
~/.config/fish/completions/videodl.fish` après création de ce dossier.
Aucun fichier de démarrage du shell n'est modifié automatiquement.

## Fichiers et reprise

Configuration : $XDG_CONFIG_HOME/videodl, sinon ~/.config/videodl.
File et journaux : $XDG_STATE_HOME/videodl, sinon ~/.local/state/videodl.
Destination par défaut : ~/Downloads/videodl.

Chaque destination contient un dossier caché `.videodl/` : petits verrous,
métadonnées et fichiers HTTP partiels. Un téléchargement HTTP interrompu peut
reprendre si le serveur fournit un ETag fort et accepte Range/If-Range. Sans ces
garanties il repart du début. Les téléchargements HLS/FFmpeg repartent du début.
Les verrous vides restent après réussite ; les données partielles sont supprimées.
Pour libérer des partiels abandonnés, arrêter les téléchargements avant de retirer
leurs fichiers .part et .json. Ne pas supprimer les verrous pendant un téléchargement.

Un fichier final existant est toujours préservé. Choisissez un autre nom pour
retélécharger. `cancel` conserve un partiel HTTP récupérable pour un futur `retry`.

## Paramètres de transfert

`retries` : 0 à 10, défaut 3. Erreurs temporaires réseau et HTTP
408/429/500/502/503/504, attente croissante, Retry-After respecté jusqu'à 5 minutes.
`timeout` : connexion/TLS/en-têtes HTTP, défaut 30s.
`idle_timeout` : lecture sans nouvelles données, défaut 60s.
`resume` : reprise HTTP vérifiée, défaut true.
`max_height` : hauteur HLS maximale (720, 1080…), 0 = meilleur débit disponible.
Une hauteur inconnue n'est pas sélectionnée lorsqu'une limite est demandée.

HLS avec pistes audio séparées, discontinuités ou byte ranges : FFmpeg requis.
HLS MPEG-TS vers .mp4/.mkv : remuxage par FFmpeg sans réencodage.
La piste audio par défaut du groupe sélectionné est utilisée.
`--max-height` concerne la sélection HLS intégrée, pas DASH ni `--ffmpeg` forcé.
Les nouvelles tentatives natives ne s'appliquent pas au processus FFmpeg.
Le mode HLS intégré traite un instantané de playlist ; il ne capture pas un direct
indéfiniment. Pour ces cas, utiliser explicitement FFmpeg.

## Dépannage

`videodl doctor` vérifie la configuration, les chemins et FFmpeg sans téléchargement.
Il ne crée pas de fichier et n'appelle aucune URL. `videodl daemon logs` consulte
les journaux du service. Une erreur de téléchargement reste attachée à sa tâche ;
le service continue. Une erreur d'accès ou de corruption de la file reste fatale.
