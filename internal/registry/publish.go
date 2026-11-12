package registry

import (
	"sort"
	"strings"

	"github.com/PierreOlivierBrillant/gh-milou/internal/naming"
	"github.com/PierreOlivierBrillant/gh-milou/internal/roster"
	"github.com/PierreOlivierBrillant/gh-milou/internal/valid"
)

// Les noms accumulés poste par poste ne montent pas d'eux-mêmes au registre :
// ils y sont déjà, dans un fichier que plus rien ne lit. La publication les y
// verse, une fois, en montrant d'abord ce qu'elle ferait.
//
// Elle n'écrase rien sans le dire. Le registre a pu être corrigé par quelqu'un
// d'autre, et le fichier local porte des noms parfois plus anciens ; le
// désaccord se montre, il ne se tranche pas en silence.
//
// Le choix a moins de conséquences qu'il n'y paraît : quel que soit le nom
// retenu, tous les slugs sont conservés, et aucun dépôt ne se détache de sa
// personne. C'est ce qui permet de publier sans craindre de se tromper.

// Disagreement est un compte que le registre et le poste ne nomment pas pareil.
type Disagreement struct {
	Username string `json:"username"`
	// Registry est le nom au registre, Local celui de ce poste.
	Registry string `json:"registry"`
	Local    string `json:"local"`
}

// Ambiguity est un compte que les listes de ce poste nomment de plusieurs
// façons — le même étudiant, saisi deux fois, deux sessions de suite.
type Ambiguity struct {
	Username string   `json:"username"`
	Names    []string `json:"names"`
	// Chosen est celui que la publication retiendrait : le premier rencontré.
	Chosen string `json:"chosen"`
}

// Publication est ce qu'une publication ferait, avant qu'elle ne le fasse.
type Publication struct {
	// New sont les personnes que le registre ne connaît pas encore.
	New []User `json:"new"`
	// Renamed sont celles qu'il connaît sous un autre nom.
	Renamed []Disagreement `json:"renamed"`
	// Known compte celles qu'il connaît déjà à l'identique.
	Known int `json:"known"`
	// Ambiguous énumère les comptes que ce poste nomme de plusieurs façons.
	Ambiguous []Ambiguity `json:"ambiguous"`
	// Nameless énumère les comptes dont personne ne connaît le nom complet.
	// Rien ne peut être publié pour eux ; les retrouver vient d'abord.
	Nameless []string `json:"nameless"`
}

// Empty dit qu'il n'y a rien à publier.
func (p Publication) Empty() bool { return len(p.New) == 0 && len(p.Renamed) == 0 }

// Count dit combien de personnes la publication toucherait.
func (p Publication) Count() int { return len(p.New) + len(p.Renamed) }

// Plan confronte au registre les personnes rassemblées sur un poste.
//
// L'ordre des personnes compte : quand un compte porte plusieurs noms, c'est le
// premier qui est retenu. L'appelant les fournit donc dans un ordre stable —
// les groupes par place —, pour que deux aperçus successifs disent la même
// chose.
func Plan(set *Set, people []roster.Person) Publication {
	rassembles, ordre := gather(people)

	publication := Publication{}
	for _, compte := range ordre {
		noms := rassembles[compte]
		if len(noms) == 0 {
			publication.Nameless = append(publication.Nameless, compte)
			continue
		}
		if len(noms) > 1 {
			publication.Ambiguous = append(publication.Ambiguous, Ambiguity{
				Username: compte, Names: noms, Chosen: noms[0],
			})
		}

		fiche := User{Username: compte, FullName: noms[0], Slugs: slugsOf(noms)}
		connue, connu := set.Find(compte)
		switch {
		case !connu:
			publication.New = append(publication.New, fiche)
		case strings.EqualFold(connue.FullName, noms[0]):
			publication.Known++
		default:
			publication.Renamed = append(publication.Renamed, Disagreement{
				Username: compte, Registry: connue.FullName, Local: noms[0],
			})
		}
	}
	return publication
}

// gather rassemble par compte les noms distincts qu'un poste lui donne, dans
// l'ordre où ils s'y présentent. Le compte garde l'orthographe de sa première
// apparition : GitHub n'y distingue pas la casse, et en changer ferait un faux
// changement à chaque publication.
func gather(people []roster.Person) (map[string][]string, []string) {
	noms := map[string][]string{}
	comptes := map[string]string{}
	var ordre []string
	for _, person := range people {
		compte := strings.TrimSpace(person.Username)
		if compte == "" {
			continue
		}
		cle := strings.ToLower(compte)
		if _, vu := comptes[cle]; !vu {
			comptes[cle] = compte
			ordre = append(ordre, compte)
		}
		nom := strings.TrimSpace(person.FullName)
		if nom == "" || contains(noms[comptes[cle]], nom) {
			continue
		}
		noms[comptes[cle]] = append(noms[comptes[cle]], nom)
	}
	return noms, ordre
}

func contains(liste []string, valeur string) bool {
	for _, item := range liste {
		if strings.EqualFold(item, valeur) {
			return true
		}
	}
	return false
}

// slugsOf rend les slugs de tous les noms qu'un compte a portés. Les garder
// tous est ce qui rend la publication sans danger : quel que soit le nom
// retenu, les dépôts créés sous les autres restent rattachés à leur personne.
func slugsOf(noms []string) []string {
	slugs := make([]string, 0, len(noms))
	for _, nom := range noms {
		if slug, err := naming.Student(nom); err == nil {
			slugs = append(slugs, slug)
		}
	}
	sort.Strings(slugs)
	return slugs
}

// Apply compose le changement qui publie ce que le plan a montré.
//
// Les désaccords ne sont repris que si on le demande : par défaut, le registre
// garde son nom — quelqu'un l'y a peut-être corrigé, et le fichier local n'est
// pas forcément le plus récent. Leurs slugs, eux, montent dans les deux cas :
// un nom qu'on n'adopte pas a tout de même nommé des dépôts.
func (p Publication) Apply(local bool) Change {
	change := Change{Reason: p.message(local)}
	change.Learn = append(change.Learn, p.New...)
	for _, desaccord := range p.Renamed {
		fiche := User{
			Username: desaccord.Username,
			Slugs:    slugsOf([]string{desaccord.Registry, desaccord.Local}),
		}
		if local {
			fiche.FullName = desaccord.Local
		}
		change.Learn = append(change.Learn, fiche)
	}
	return change
}

// message dit ce que le commit racontera.
func (p Publication) message(local bool) string {
	parties := make([]string, 0, 2)
	if n := len(p.New); n > 0 {
		parties = append(parties, valid.Plural(n, "étudiant"))
	}
	if n := len(p.Renamed); n > 0 {
		verbe := "nom conservé"
		pluriel := "noms conservés"
		if local {
			verbe, pluriel = "nom repris du poste", "noms repris du poste"
		}
		parties = append(parties, valid.Plural(n, verbe, pluriel))
	}
	if len(parties) == 0 {
		return "Publie le registre"
	}
	return "Publie le registre : " + strings.Join(parties, ", ")
}
