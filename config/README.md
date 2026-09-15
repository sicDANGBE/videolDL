# Configuration et premier lancement

Depuis la racine du projet :

```sh
./videodl setup --destination "$HOME/Videos/videodl"
./videodl doctor
./videodl help
```

`setup` prépare `~/.config/videodl/` (ou le répertoire XDG correspondant) :

```text
config.json                  paramètres effectifs personnalisés
README.md                    guide d'utilisation
videodl.1                    page de manuel
completions/videodl.bash      autocomplétion Bash
completions/videodl.zsh       autocomplétion Zsh
completions/videodl.fish      autocomplétion Fish
```

La configuration existante est conservée. Pour changer la destination ensuite :
`./videodl config set destination /chemin/absolu`.
Aucun téléchargement ou service ne démarre pendant setup.

`config.example.json` est un exemple minimal valide : les chemins omis prennent
les valeurs personnalisées de l'utilisateur. Utilisez `setup` pour générer votre
configuration complète ; ne modifiez pas l'exemple pour configurer le programme.
Une ancienne configuration reste acceptée, les nouveaux paramètres ont des valeurs
par défaut.

Pour un profil séparé, utilisez `./videodl setup --config /chemin/config.json`,
puis passez ce même `--config` à chaque commande concernée. Les chemins relatifs
sont résolus depuis le dossier de lancement, pas depuis celui du fichier JSON.

Consulter : `config path`, `config show`, `config get CLE`.
Modifier : `config set CLE VALEUR` ou `config edit`.
Ordre : valeurs par défaut < JSON < variables VIDEODL_* < options de commande.
Les options de transfert explicitement données à `add` sont conservées sur la tâche ;
les options explicites de `worker` ont priorité sur elles.
`config show` masque les commandes de notification et adresses de webhook.
Le dossier de configuration doit rester privé.

Installation facultative du programme, du manuel et des completions :

```sh
./bin/install.sh
```

Le préfixe par défaut est `~/.local`. Aucun fichier de démarrage du shell n'est
modifié. Le guide créé par setup explique l'activation de la complétion.
