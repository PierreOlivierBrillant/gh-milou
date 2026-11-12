# Milou

[![tests](https://github.com/PierreOlivierBrillant/gh-milou/actions/workflows/tests.yml/badge.svg)](https://github.com/PierreOlivierBrillant/gh-milou/actions/workflows/tests.yml)

Extension [GitHub CLI](https://cli.github.com) qui reproduit GitHub Classroom
pour une personne qui enseigne : **un dépôt par étudiant** dans une
organisation, créé à partir d'une liste « nom complet + compte GitHub », puis la
gestion de ce qui existe déjà — accès, clonage, mises à jour, suppression.
Milou, comme le fox-terrier de Tintin : il court chercher chaque dépôt, le
rapporte, et ne perd personne en route.

Trois façons de s'en servir, pour les mêmes opérations : une interface web
servie sur la boucle locale, un assistant au terminal, et des drapeaux
scriptables.

```bash
gh milou          # interface web (défaut)
gh milou --cli    # assistant au terminal
```

Écrite en Go et distribuée précompilée : aucune installation de Go n'est
nécessaire. Toute l'interface est en français.

## Installation

```bash
gh extension install PierreOlivierBrillant/gh-milou
```

Mise à jour :

```bash
gh extension upgrade milou
```

L'outil s'appelait `gh cohorte`. Une installation sous l'ancien nom se
remplace — `gh extension remove cohorte`, puis l'installation ci-dessus — et
les réglages, les groupes déclarés et le cache sont repris tels quels.

### Prérequis

- **`gh` authentifié** (`gh auth login`) : l'extension reprend son jeton, son
  hôte et ses limites de débit.
- **Portées** : `repo` pour créer les dépôts et inviter les personnes,
  `read:org` pour lister vos organisations. À la demande : `admin:org` pour les
  travaux d'équipe, `delete_repo` pour supprimer un dépôt, `workflow` pour
  déposer des fichiers dans `.github/workflows`
  (`gh auth refresh -s admin:org,delete_repo,workflow`).
- **`git`**, uniquement pour cloner et mettre à jour des clones.

Le droit de créer des dépôts dans l'organisation visée est requis ; un rôle
insuffisant est signalé avant toute tentative.

## Démarrage rapide

Lancée sans argument, l'extension ouvre l'interface web dans le navigateur.
Tout y commence par le choix d'une organisation, puis se parcourt de haut en
bas :

```
session  →  cours  →  groupe  →  travail
a26         5N6        01        tp1
```

Un **groupe** rassemble des **étudiants**, à qui l'on **distribue des travaux** :
c'est le modèle de GitHub Classroom, avec ses mots. Chaque niveau a son adresse,
si bien qu'un lien ou un rechargement de page ramène au même endroit.

Au terminal, l'assistant enchaîne authentification → organisation → liste des
personnes → vérification des comptes → paramètres → récapitulatif →
confirmation → création → bilan :

```bash
gh milou --cli
```

Pour voir ce qui serait fait, sans rien créer :

```bash
gh milou --roster cohorte.csv --dry-run
```

Pour une exécution scriptée, sans aucune question :

```bash
gh milou --org acme --assignment tp1 --roster cohorte.csv --non-interactive --yes
```

En mode non interactif, une valeur requise mais absente est une erreur explicite
plutôt qu'une invite laissée en suspens.

## La liste des étudiants

Deux colonnes : le nom complet et le compte GitHub. Les en-têtes sont reconnus
en français comme en anglais (`nom_complet`/`name`, `github_username`/`login`…),
et le séparateur (`,`, `;` ou tabulation) est détecté automatiquement.

```csv
nom_complet,github_username
Émilie Côté,emilie-cote
Jean-Luc Picard,jlpicard
Aminata Diallo,aminata-d
```

Un exemple complet se trouve dans [`examples/cohorte.csv`](examples/cohorte.csv).
Les lignes vides et celles commençant par `#` sont ignorées ; chaque ligne
rejetée est signalée avec son numéro et la raison, et le reste du fichier
continue d'être lu.

**La liste d'Omnivox se lit telle quelle**, sans rien convertir : son encodage
Windows-1252, ses champs `="…"` et ses colonnes séparées « Nom » et « Prénom »
sont reconnus. Dans Léa : *Liste des étudiants › Paramètres d'affichage*, mode
« Pour Excel », séparateur `;`, et cochez **Numéro d'étudiant**, **Nom de
l'étudiant** et **Code permanent**.

Elle ne dit pas les comptes GitHub, et ce n'est pas un obstacle : la cohorte
s'inscrit telle quelle, chacun désigné par son matricule. Son dépôt est créé —
c'est son nom qui le nomme —, et le bilan dit qu'il reste un compte à rattacher.
Ajoutez-lui une colonne `GitHub` si vous les connaissez ; sinon, reprendre des
dépôts existants rapproche les comptes des noms et des matricules, et montre
chaque rapprochement — avec ce qui l'a produit — avant d'écrire.

Rattacher un compte à quelqu'un qui n'en avait pas ne se fait que dans
l'interface web : le terminal et la ligne de commande n'offrent pas ce geste.

## Reprendre des dépôts existants

Des dépôts nommés `travail-compte`, comme GitHub Classroom les laisse, se
reprennent d'un bloc : l'outil lit les travaux que leurs préfixes dessinent,
rapproche les comptes des étudiants de la liste, puis renomme vers la
nomenclature. GitHub garde une redirection depuis chaque ancien nom.

Un travail fait en équipe se reprend de même, en disant que ce qui suit le
préfixe nomme une équipe et non une personne : il n'y a alors aucune liste à
rapprocher, les membres venant des accès au dépôt.

```bash
gh milou --import                                    # lister les travaux repérés
gh milou --import tp1 --into a26.5n6.1030 --roster liste.csv --dry-run
gh milou --import projet --teams --into a26.5n6.1030  # travail d'équipe
```

## Nommage des dépôts

Un dépôt porte **cinq niveaux, séparés par un point** :

```
session . cours . groupe . travail . étudiant
a26.5n6.01.tp1.emilie-cote
```

Le point est réservé à cette découpe : la slugification remplace tout caractère
non alphanumérique par un tiret, si bien qu'un nom venu d'un CSV en est nettoyé
(« J.-P. Tremblay » devient `j-p-tremblay`) et qu'un compte GitHub n'en contient
jamais. Un nom se relit donc sans rien deviner.

Le dernier niveau nomme le **destinataire** du dépôt. Pour un travail
individuel, c'est le **nom de l'étudiant**, pas son compte GitHub : un dépôt se
lit sans connaître le pseudonyme de personne. En contrepartie, le nom complet
est obligatoire et deux homonymes font échouer la préparation avant toute
écriture — à moins que le matricule ne dise qu'il s'agit de la même personne
sous deux comptes. C'est lui, et lui seul, qui identifie quelqu'un : le nom ne
distingue pas deux homonymes, et rien ne rapproche deux comptes d'une même
personne.

Pour un travail d'équipe, c'est le nom de l'**équipe** — `a26.5n6.01.projet.eq1`.
Une équipe est une vraie équipe d'organisation GitHub, et son nom porte lui
aussi la place du groupe (`a26.5n6.01.eq1`) : une organisation n'accepte qu'un
nom d'équipe donné, et sans cette place deux groupes ne pourraient pas avoir
chacun leur « eq1 ». Un travail est donc d'équipe ou individuel selon ce que son
dernier niveau nomme, et rien n'est déclaré ailleurs.

**GitHub reste la seule source de vérité.** Sessions, cours, groupes et travaux
se lisent dans le nom des dépôts : un groupe n'a rien à déclarer pour exister.

Le dernier niveau, lui, est un nom slugifié — rien n'y dit à quel compte il
appartient. C'est ce que retient le **registre** : un dépôt privé `.cohorte` de
l'organisation, un nom complet par compte et le rôle tenu — étudiant ou
enseignant —, écrit une fois pour tout le monde. Vos collègues voient donc les
mêmes noms que vous sans rien avoir déclaré, et corriger une orthographe ne
détache pas les dépôts créés sous l'ancienne. Les noms déjà accumulés sur un
poste s'y versent en une fois (`gh milou --publish-registry`). Le registre
porte aussi la **date de remise** de chaque travail, que rien dans un nom de
dépôt ne peut dire : une échéance fixée sur un poste vaut pour l'équipe entière.
Le fichier local ne garde plus que ce qui n'a de sens que sur cette machine :
les groupes déclarés ici, et les réglages du dernier travail.

**Deux enseignants ne voient pas les étudiants l'un de l'autre.** Un groupe peut
porter une équipe `a26.5n6.01.enseignants`, qui reçoit ses dépôts : ceux qui n'en
sont pas ne les voient pas. Le registre dit *qui* enseigne, l'équipe dit *où* —
et c'est l'équipe, jamais le registre, qui ouvre l'accès. Une équipe ouvre un
accès sans en fermer aucun : pour que le cloisonnement tienne, les enseignants
doivent être membres de l'organisation et non propriétaires, et sa permission de
base doit être `none`.

**Comparer avec un collègue ne demande pas de percer ce cloisonnement.** Ce qui
circule est un **index d'empreintes** (`--publish-index`, `--against`) : des
hachés de fragments, sous des jetons tirés au hasard, dans un second dépôt privé
`.cohorte-empreintes`. On mesure les ressemblances avec les copies d'en face
sans en lire une ligne ni savoir de qui elles sont. Un catalogue — dans le
registre — dit qui a donné quel travail, à combien de personnes : de quoi savoir
à qui s'adresser, sans qu'aucune liste de classe ne soit publiée.

Le voile ne se lève que sur un geste : une **demande** nommée (`--ask`), que le
propriétaire des copies accorde ou refuse (`--grant`, `--deny`). S'il accorde,
l'outil lui prépare l'archive anonymisée de la seule copie demandée, et c'est
lui qui l'envoie. Aucune passe automatisée ne tranche à sa place.

Un groupe se déplace d'une place à l'autre — une autre session, un autre cours,
un autre numéro — en renommant ses dépôts, avec un aperçu avant écriture.
GitHub garde une redirection depuis chaque ancien nom : les clones et les liens
déjà distribués continuent de fonctionner.

## Ce que fait l'outil

- **Distribuer un travail** : dépôt modèle (`--template`) ou dossier de fichiers
  de départ (`--starter`) déposé en un seul commit par l'API Git, sans clone
  local. Un dépôt déjà garni n'est jamais réécrit ; un dépôt resté vide est
  complété à la relance. Un squelette d'exemple :
  [`examples/depart/`](examples/depart).
- **Gérer les accès** : invitations, collaborateurs, droit accordé
  (`--permission`).
- **Cloner et mettre à jour** : en parallèle (`--jobs`), par `git pull
  --ff-only` — un dossier où vous avez travaillé remonte en échec plutôt que
  d'être modifié — et sans jamais écrire le jeton dans une URL ou dans
  `.git/config`, l'authentification passant par `gh auth git-credential`.
- **Filtrer, trier, chercher** les listes d'étudiants et de travaux, à
  l'identique dans les trois interfaces (`--filter`, `--pushed-after`,
  `--pushed-before`, `--never-pushed`, `--sort`).
- **Déplacer un travail ou des étudiants** d'un groupe à l'autre : leurs dépôts
  sont renommés, puisque c'est leur nom qui dit à quel groupe ils appartiennent.
- **Distribuer un travail en équipe** : un dépôt par équipe, partagé avec elle
  plutôt qu'avec chacun de ses membres — changer sa composition suffit donc à
  changer qui y accède. Les équipes se créent, se renomment, se suppriment, et
  une équipe déjà présente dans l'organisation s'adopte telle quelle.
- **Comparer les copies d'un travail entre elles** (`--plagiarism`) : elles sont
  mesurées deux à deux et classées par ordre de suspicion, le gabarit distribué
  étant retiré d'office puisque l'outil sait lequel il a déposé. **Ce n'est pas
  un détecteur de plagiat** : une ressemblance forte n'est pas une preuve — elle
  se vérifie en lisant les passages communs, et elle s'explique parfois. L'outil
  le redit sur chaque écran, et ce n'est pas une formule de politesse.
  Chaque dépôt distribué reçoit en outre une **marque invisible** propre à son
  destinataire : un jeton tiré au hasard, écrit en espaces et en tabulations sur
  une ligne vide du README. Rien ne se voit, l'étudiant ne peut ni la lire ni la
  deviner, et deux travaux qui portent la même n'ont pas d'explication
  innocente. Son absence, en revanche, ne prouve rien : un formateur l'efface
  sans le savoir. Elle se désactive dans les réglages, ou par `--no-sign`.
  La comparaison porte sur le groupe, sur le cours, ou sur toutes les sessions
  (`--reach`). Un cours qui a changé de sigle n'est retrouvé que si on l'a dit :
  les équivalences — « 5N6 est devenu 5M6 » — se déclarent une fois dans le
  registre de l'organisation, et valent alors pour toute l'équipe.
  Avec un collègue d'une autre organisation, ce sont des **copies anonymisées**
  qui s'échangent (`--export-zip`, `--import-zip`) : noms, comptes et matricules
  y sont remplacés par des jetons de longueur égale, et la table de
  correspondance reste chez l'expéditeur. L'anonymisation n'est jamais
  complète — ce qui lui a résisté est montré avant que l'archive ne parte.

Ce que GitHub Classroom fait et que l'outil ne fait pas : pas de lien
d'invitation à distribuer — les dépôts sont créés directement —, pas de
correction automatique.

L'assistant du terminal travaille par préfixe (`--manage tp1`) : il lit les
groupes — leurs listes, leurs dates cibles — sans les tenir. Déclarer un groupe,
tenir sa liste d'étudiants ou déplacer une personne n'existent donc que dans
l'interface web. Les équipes, elles, appartiennent à un groupe : au terminal,
c'est la place du groupe qui en tient lieu (`--manage a26.5n6.01 --teams`). La
comparaison des copies se lance des trois façons, mais ses deux vues côte à
côte — deux projets, deux fichiers — n'existent que dans l'interface web :
surligner un passage commun et sauter au suivant n'a pas d'équivalent
scriptable. Seule l'interface web renomme ou déplace un groupe entier et retient
ses réglages par défaut : en ligne de commande, les drapeaux ne valent que pour
un lancement. Les règles de comparaison s'écrivent dans l'interface web ou dans
un fichier (`--rules`, `--publish-rules`), pas dans l'assistant, qui ne publie
que l'index du travail géré : ceux de toute l'organisation se publient dans
l'interface web ou par `--publish-index` seul. Tout le reste est disponible
partout.

## Fiabilité

L'outil est **idempotent** : un dépôt existant est signalé « déjà présent »,
jamais recréé ni écrasé, et relancer la même commande après correction reprend
là où le lot s'était arrêté. Une erreur sur une personne n'interrompt pas le
reste : les échecs sont collectés et rapportés à la fin, et chaque exécution
dépose un bilan JSON et CSV dans `rapports/`.

Rien n'est écrit sans un récapitulatif suivi d'une confirmation explicite. Sont
vérifiés d'avance : l'existence des comptes et de l'organisation, la validité
des noms de dépôts, les collisions entre deux personnes, le drapeau
`is_template` du dépôt modèle, la taille du dossier de départ.

Les inventaires d'organisation sont mis en cache dans le répertoire du système
(`~/.cache/milou/cache.json` sous Linux, permissions `600`) ; `--no-cache` et
`--clear-cache` s'en passent ou le vident.

## Sécurité

- Le jeton n'est jamais affiché, journalisé ni écrit sur le disque : il vient de
  `gh` et ne sert qu'aux en-têtes HTTP.
- L'interface web n'écoute que sur `127.0.0.1`, sur un port tiré au hasard, et
  n'accepte que les requêtes portant un jeton de session lui aussi tiré au
  hasard — l'origine et l'en-tête `Host` sont vérifiés, si bien qu'un autre site
  ouvert dans le même navigateur ne peut rien déclencher.
- Les actions destructives demandent une confirmation explicite ; supprimer un
  dépôt exige d'en écrire le nom exact — retapé dans l'assistant ou l'interface
  web, ou donné en entier à `--delete-repo` —, et aucune option, `--yes`
  compris, ne court-circuite cette confirmation.
- Les données d'étudiants (bilans, listes, clones) sont exclues du dépôt par le
  `.gitignore`. Le registre, lui, vit dans un dépôt privé de l'organisation :
  l'outil refuse d'y écrire s'il devient public, signale une permission de base
  qui l'ouvrirait aux étudiants membres, sait en donner l'accès à une équipe
  enseignante et en réécrire l'historique.

## Options

Les plus courantes :

| Drapeau | Effet |
| --- | --- |
| `--org ORG` | organisation GitHub cible |
| `--roster FICHIER` | liste « nom complet, compte GitHub » au format CSV |
| `--assignment NOM` | identifiant du travail |
| `--manage [PREFIXE]` | gérer un groupe existant au lieu d'en créer un |
| `--plagiarism` | comparer entre elles les copies du travail géré |
| `--reach PORTEE` | jusqu'où comparer : `groupe`, `cours`, `annees` |
| `--export-zip FICHIER` | archive anonymisée des copies, à envoyer à un collègue |
| `--publish-index` | publier l'index d'empreintes du travail géré |
| `--against TRAVAUX` | travaux d'un collègue à comparer, par leur identifiant |
| `--teams` | travail d'équipe ; avec `--manage`, les équipes du groupe |
| `--team NOM` | équipe visée, ou équipes à servir |
| `--import [TRAVAIL]` | reprendre des dépôts nommés « travail-compte » |
| `--into PLACE` | place d'arrivée d'une reprise (« a26.5n6.1030 ») |
| `--template ORG/DEPOT` | dépôt modèle |
| `--starter DOSSIER` | dossier local déposé dans chaque dépôt, en un commit |
| `--dry-run` | simuler sans rien créer |
| `-y`, `--yes` | passer la confirmation finale |
| `--non-interactive` | échouer plutôt que poser une question |
| `--cli` / `--no-browser` | rester au terminal / ne pas ouvrir le navigateur |

`gh milou --help` donne la liste complète. Codes de retour : `0` succès, `1`
au moins un échec, `2` erreur de validation, `130` interruption.

Trois variables d'environnement : `NO_COLOR` retire la couleur,
`MILOU_NO_ARROWS` force les listes numérotées, `MILOU_NO_SHELL_COMPLETION`
complète les chemins sans interroger le shell.

## Développement

```bash
go build .               # produit ./gh-milou
go test ./...            # toute la suite, sans aucun accès réseau
gh extension install .   # installer la version locale
```

Les tests montent un faux serveur GitHub local (`internal/fakegh`) et de vrais
dépôts git locaux (`file://`) : rien ne sort de la machine.

La logique vit dans les paquets du domaine — `internal/naming` (la
nomenclature), `internal/classroom` (les groupes), `internal/teams` (les
équipes), `internal/registry` (le
registre des utilisateurs), `internal/plan`, `internal/groups`, `internal/roster`,
`internal/users`, `internal/runner`, `internal/clone` — et les trois
interfaces (`internal/web`, `internal/app`) n'en sont que des façades. C'est ce
qui garantit qu'elles ne divergent pas.
[`CLAUDE.md`](CLAUDE.md) énonce les règles à ne pas perdre de vue.

Publication : pousser une étiquette `vX.Y.Z` déclenche le workflow
[`release`](.github/workflows/release.yml), qui construit linux, macOS et
Windows en amd64 comme en arm64.
