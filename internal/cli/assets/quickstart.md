# videodl — première utilisation

Ce dossier contient votre `config.json`, ce guide, le manuel `videodl.1` et
les scripts d'autocomplétion `completions/` pour Bash, Zsh et Fish.
`videodl setup` complète les fichiers manquants sans remplacer vos réglages.
`setup --destination DIR` modifie uniquement la destination, même si le fichier
existe déjà. `videodl` affiche les réglages effectifs et les commandes utiles.
Aucun service ne démarre pendant la configuration. Les tâches déjà ajoutées gardent
leur chemin de sortie ; la nouvelle destination concerne les prochains ajouts.

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
    videodl config set min_free_space 2GiB
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

## Installation et mises à jour

Depuis un dépôt à jour, la même commande sert à installer et à mettre à jour :

```sh
./bin/install.sh
```

Un préfixe différent peut être fourni : `./bin/install.sh /chemin/absolu`.
Le script compile le programme et son installateur temporaire avant d’interrompre
un service. Il prépare le binaire, le manuel et les trois complétions avant le
remplacement.

Lors d’une mise à jour, il repère les daemons de **cet exécutable installé**, les
arrête proprement, remplace les fichiers, puis relance uniquement ceux qui étaient
actifs. Leurs profils, dossiers de lancement et variables d’environnement sont
conservés sans être affichés. Le redémarrage est confirmé par la prise en charge
de la file. Un service arrêté reste arrêté ; il est inutile de relancer `setup`.
Les services d’une autre copie de videodl ne sont pas concernés.

Les tâches interrompues proprement sont remises en attente. HTTP reprend le partiel
si les conditions habituelles le permettent (ETag fort et serveur compatible).
HLS et FFmpeg recommencent la tâche depuis le début. Aucun fichier final existant
n’est écrasé. La configuration n’est pas réinitialisée et la file n’est pas remplacée.

Terminez les `worker` au premier plan avant l’installation : leur présence bloque
le remplacement avec un message explicite. Deux installations simultanées du même
préfixe sont refusées. Le script n’effectue pas de `git pull` et ne lance pas de
nouveau service si aucun n’était actif.

Si la préparation échoue, le service reste actif. Après un échec de remplacement,
de redémarrage ou une interruption gérée, l’installateur tente de restaurer les
fichiers précédents et les services qu’il avait arrêtés. Il signale tout échec de
restauration et conserve les sauvegardes nécessaires. La progression de la file
n’est jamais restaurée depuis une ancienne copie. Ce mécanisme ne couvre pas une
coupure électrique ou SIGKILL ; il ne migre pas les schémas de données.

Les mises à jour se font avec le même utilisateur, sous Linux. Pour une copie du
programme que vous remplacez manuellement, arrêtez son daemon avant le remplacement.

## Dépannage

`videodl doctor` vérifie la configuration, les chemins et FFmpeg sans téléchargement.
Il ne crée pas de fichier et n'appelle aucune URL. `videodl daemon logs` consulte
les journaux du service. Une erreur de téléchargement reste attachée à sa tâche ;
le service continue. Une erreur d'accès ou de corruption de la file reste fatale.
