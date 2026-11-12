package groups

import (
	"sort"
	"strings"
	"time"

	"github.com/PierreOlivierBrillant/gh-milou/internal/valid"
)

// Un nom de dépôt dit à qui il appartient ; il ne dit pas ce qu'on y a mis.
// Savoir si un travail a été rendu, par qui, et quand demande son historique —
// la seule chose que l'inventaire d'une organisation ne porte pas.
//
// « pushed_at », que l'inventaire donne pourtant, ne répond pas à la question.
// Un envoi n'est pas un commit : renommer une branche, pousser une étiquette
// ou forcer une réécriture avancent la date sans que rien n'ait été écrit, et
// un commit ancien poussé tard porte la date du jour. Ce qu'on affiche à côté
// d'une date de remise doit être la date du commit, pas celle de son voyage.

// Author est un auteur que l'historique porte sans qu'aucun compte GitHub n'y
// réponde : l'adresse de courriel du commit n'est rattachée à personne.
type Author struct {
	Name    string `json:"name,omitempty"`
	Email   string `json:"email,omitempty"`
	Commits int    `json:"commits"`
}

// Handin est ce que l'historique d'un dépôt dit d'une remise : combien de
// commits, quand fut le dernier, et qui les a faits.
//
// Tout s'y rapporte à la branche par défaut. C'est celle que GitHub montre, et
// celle que l'étudiant croit remettre ; compter les commits d'une branche
// oubliée ferait dire à l'écran qu'un travail a été rendu alors que personne
// n'ira l'y chercher.
type Handin struct {
	// Commits est le nombre de commits de la branche par défaut.
	Commits int `json:"commits"`
	// Last est la date du commit le plus récent, au format RFC 3339. Elle est
	// vide quand le dépôt n'a rien reçu.
	Last string `json:"last,omitempty"`
	// LastBy date le commit le plus récent de chaque compte, au même format.
	// Les clés sont en minuscules, et la clé vide porte les commits qu'aucune
	// adresse ne rattache à un compte.
	//
	// Elle ne couvre que les commits les plus récents de la branche, et cela
	// suffit : un auteur qui n'y paraît pas a forcément commis plus tôt que le
	// plus ancien de ceux-là, et ne peut donc être le dernier de personne.
	LastBy map[string]string `json:"last_by,omitempty"`
	// Authors compte les commits de chaque compte GitHub. Les clés sont en
	// minuscules : GitHub ne distingue pas la casse d'un compte.
	Authors map[string]int `json:"authors,omitempty"`
	// Anonymous porte ce que l'historique sait des auteurs sans compte. Un
	// étudiant qui commet depuis une machine mal configurée est là, et nulle
	// part ailleurs : l'oublier le ferait passer pour muet.
	Anonymous []Author `json:"anonymous,omitempty"`
	// Seen est l'instant où l'historique a été lu, au format RFC 3339. C'est
	// lui, et non l'âge de l'entrée en mémoire, qui dit si le relevé est encore
	// complet : tant que rien n'a été poussé depuis, il l'est.
	Seen string `json:"seen,omitempty"`
}

// seenSkew est la marge accordée aux horloges. « Seen » vient de la machine,
// « pushed_at » de GitHub, et un poste mal réglé de quelques secondes ne doit
// pas faire croire qu'on a poussé après un relevé qui a suivi l'envoi. La
// marge a son revers : un envoi fait dans la minute qui suit le relevé passe
// pour couvert, jusqu'au relevé suivant.
const seenSkew = time.Minute

// Covers dit que l'historique relevé couvre tout ce que le dépôt a reçu : rien
// n'y a été poussé depuis sa lecture. Un relevé sans date — lu avant qu'on la
// retienne — ne couvre rien.
func (h Handin) Covers(pushedAt string) bool {
	vu, err := time.Parse(time.RFC3339, h.Seen)
	if err != nil {
		return false
	}
	if strings.TrimSpace(pushedAt) == "" {
		return true
	}
	envoi, err := time.Parse(time.RFC3339, pushedAt)
	if err != nil {
		return false
	}
	return !envoi.After(vu.Add(seenSkew))
}

// Complete rend, parmi les historiques relevés, ceux qui couvrent encore tout
// ce que leur dépôt a reçu. C'est ce qu'un écran peut montrer comme relevé.
//
// L'âge n'y entre pas. Un historique que rien n'a dépassé dit encore la vérité
// une journée plus tard, et un historique qu'un envoi a dépassé ne la dit plus
// une minute après. Le périmer à l'heure ferait disparaître d'un écran des
// relevés exacts sans qu'aucun geste l'ait demandé — c'est l'inventaire qui
// sait si quelque chose a bougé. Un dépôt absent de l'inventaire garde son
// historique : rien ne dit qu'il a reçu quoi que ce soit.
func Complete(repos []RepoInfo, remises map[string]Handin) map[string]Handin {
	envois := make(map[string]string, len(repos))
	for _, repo := range repos {
		envois[strings.ToLower(repo.Name)] = repo.PushedAt
	}
	completes := make(map[string]Handin, len(remises))
	for nom, remise := range remises {
		if remise.Covers(envois[strings.ToLower(nom)]) {
			completes[nom] = remise
		}
	}
	return completes
}

// Overtaken sépare des dépôts selon leur historique relevé : ceux qu'un envoi a
// dépassés depuis leur lecture, et tous les autres — jamais relevés, ou relevés
// et encore complets. Les premiers sont à relire d'office, si récent que soit
// leur relevé : la mémoire seule les croirait encore bons, et c'est
// l'inventaire qui sait que quelque chose a bougé. Les autres ne sont à lire
// que s'ils manquent.
func Overtaken(noms []string, repos []RepoInfo,
	histoires map[string]Handin) (depasses, autres []string) {
	completes := Complete(repos, histoires)
	for _, nom := range noms {
		_, releve := histoires[nom]
		_, complete := completes[nom]
		if releve && !complete {
			depasses = append(depasses, nom)
			continue
		}
		autres = append(autres, nom)
	}
	return depasses, autres
}

// Activity rend l'inventaire tel que les étudiants l'ont fait : le dernier
// envoi de chaque dépôt y ignore ce que les comptes écartés — ceux qui
// enseignent — y ont poussé.
//
// « pushed_at » ne dit pas qui a poussé : un fichier que l'enseignant dépose
// dans tous les dépôts ferait passer tout le groupe pour actif le même jour.
// L'historique le dit, et il remplace la date dès qu'il est relevé et que rien
// n'a été poussé depuis. Un dépôt dont on ne l'a pas lu, ou qui a reçu
// quelque chose depuis, garde « pushed_at » : mieux vaut une date trop
// récente qu'une activité d'étudiant passée sous silence.
//
// La date rendue est alors celle du commit, pas celle de son envoi — la même
// que celle de la remise, ce qui fait dire aux deux colonnes la même chose.
// L'inventaire donné n'est pas modifié : il est mis en cache tel que GitHub
// l'a rendu, et d'autres s'y fient — la réutilisation d'une analyse, entre
// autres, veut savoir si le dépôt a changé, quel qu'en soit l'auteur.
func Activity(repos []RepoInfo, remises map[string]Handin, ignore func(login string) bool) []RepoInfo {
	if ignore == nil || len(remises) == 0 {
		return repos
	}
	corriges := make([]RepoInfo, len(repos))
	for index, repo := range repos {
		if remise, lue := remises[repo.Name]; lue && remise.Covers(repo.PushedAt) {
			repo.PushedAt = remise.LastBut(ignore)
		}
		corriges[index] = repo
	}
	return corriges
}

// Empty dit qu'aucun commit n'a été relevé dans le dépôt.
func (h Handin) Empty() bool { return h.Commits == 0 }

// LastBut rend la date du commit le plus récent dont l'auteur n'est pas écarté.
//
// C'est ce qui date une remise. Un gabarit poussé à l'ouverture du travail, une
// correction déposée après coup, une note ajoutée au dépôt une fois l'échéance
// passée sont l'œuvre de qui enseigne : les compter daterait la remise du jour
// où l'enseignant y a touché, et mettrait l'étudiant en retard pour cela.
//
// Sans auteurs relevés, la date du dernier commit est tout ce qu'on sait, et
// c'est elle qui est rendue : mieux vaut une date trop tardive que pas de date
// du tout.
func (h Handin) LastBut(ignore func(login string) bool) string {
	if ignore == nil || len(h.LastBy) == 0 {
		return h.Last
	}
	dernier := ""
	for login, quand := range h.LastBy {
		if login != "" && ignore(login) {
			continue
		}
		// Les dates sont en UTC et de forme fixe : les comparer comme du texte
		// les range dans l'ordre du temps.
		if quand > dernier {
			dernier = quand
		}
	}
	return dernier
}

// CommitsBut compte les commits de la branche que les auteurs écartés n'ont pas
// signés.
//
// C'est la même règle que pour la date : les fichiers de départ et les
// corrections de qui enseigne ne sont pas le travail de l'étudiant. Les compter
// ferait voir « 2 commits » à côté d'un dernier envoi « jamais », dans un dépôt
// où personne n'a rien remis.
//
// Sans auteurs relevés, le total est tout ce qu'on sait, et c'est lui qui est
// rendu.
func (h Handin) CommitsBut(ignore func(login string) bool) int {
	if ignore == nil || len(h.Authors) == 0 {
		return h.Commits
	}
	ecartes := 0
	for login, nombre := range h.Authors {
		if login != "" && ignore(login) {
			ecartes += nombre
		}
	}
	// Les auteurs viennent d'une autre lecture que le total : un décalage entre
	// les deux ne doit pas donner un nombre de commits négatif.
	if ecartes >= h.Commits {
		return 0
	}
	return h.Commits - ecartes
}

// By compte les commits d'une personne, tous ses comptes confondus.
func (h Handin) By(accounts []string) int {
	total := 0
	for _, compte := range accounts {
		total += h.Authors[strings.ToLower(strings.TrimSpace(compte))]
	}
	return total
}

// Strangers nomme les comptes qui ont commis dans le dépôt sans y être
// attendus : ni l'un des comptes donnés, ni quelqu'un que « ignore » écarte —
// qui enseigne. Le plus souvent, c'est la personne attendue elle-même sous un
// autre compte : un courriel resté configuré sur un vieux compte suffit, et
// GitHub attribue le commit à ce compte-là, quel que soit celui qui a poussé.
// Parfois c'est quelqu'un d'autre. Dans les deux cas, c'est à qui enseigne de
// trancher, et il faut d'abord qu'il le voie.
func (h Handin) Strangers(expected []string, ignore func(login string) bool) []string {
	attendus := make(map[string]bool, len(expected))
	for _, compte := range expected {
		attendus[strings.ToLower(strings.TrimSpace(compte))] = true
	}
	etrangers := make([]string, 0)
	for login := range h.Authors {
		if login == "" || attendus[login] || (ignore != nil && ignore(login)) {
			continue
		}
		etrangers = append(etrangers, login)
	}
	sort.Strings(etrangers)
	return etrangers
}

// Signed dit si un auteur sans compte porte ce nom ou cette adresse. C'est le
// dernier recours pour rattacher un commit à quelqu'un : le nom écrit dans le
// commit est celui que la personne a donné à git, et il vaut souvent le sien.
//
// La comparaison passe par la slugification, celle-là même qui nomme les
// dépôts : « Émilie Côté », « emilie.cote@college.qc.ca » et « emilie-cote »
// désignent alors la même personne sans qu'on ait à traiter les accents ici.
func (h Handin) Signed(fullName string) bool {
	slug := valid.Slugify(fullName)
	if slug == "" {
		return false
	}
	for _, auteur := range h.Anonymous {
		if valid.Slugify(auteur.Name) == slug {
			return true
		}
		// « prenom.nom@… » : la partie locale d'une adresse porte souvent le
		// nom, et la comparer coûte moins qu'une question à qui enseigne.
		if local, _, coupe := strings.Cut(auteur.Email, "@"); coupe &&
			valid.Slugify(local) == slug {
			return true
		}
	}
	return false
}
