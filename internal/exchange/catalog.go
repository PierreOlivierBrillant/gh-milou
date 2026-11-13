// Package exchange porte ce qui circule entre enseignants d'une même
// organisation, et rien de plus.
//
// Deux enseignants ne voient pas les étudiants l'un de l'autre : c'est une
// équipe GitHub qui le garantit, et c'est voulu. Mais un travail qui circule
// entre deux sections circule quand même, et aucun des deux ne peut le voir
// seul.
//
// Trois choses suffisent à le rendre visible sans rien percer :
//
//  1. Un catalogue dit qui a donné quel travail, à combien de personnes. Il ne
//     nomme aucun étudiant et ne cite aucun dépôt : la place, le nom du travail,
//     un décompte.
//  2. Un index d'empreintes se publie. Ce sont des hachés de k-grammes — on ne
//     remonte pas au code depuis eux —, sous des jetons opaques. Un collègue
//     mesure alors les ressemblances avec ses copies sans lire une ligne des
//     nôtres.
//  3. Quand une paire sort du lot, une demande explicite ouvre les fragments,
//     et son propriétaire l'approuve. Le voile ne se lève que là, et la trace
//     en reste.
//
// Le cloisonnement tient donc pendant tout le dépistage, et ne cède que sur un
// geste délibéré, pour une paire nommée.
package exchange

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/PierreOlivierBrillant/gh-milou/internal/naming"
	"github.com/PierreOlivierBrillant/gh-milou/internal/valid"
)

// Version est celle des schémas écrits.
const Version = 1

// CatalogFile porte le catalogue, dans le registre de l'organisation.
const CatalogFile = "enseignements.json"

// Teaching est une ligne du catalogue : un travail donné par quelqu'un.
//
// Ce qu'elle ne porte pas compte autant que ce qu'elle porte. Pas de nom
// d'étudiant, pas de nom de dépôt — le dernier niveau d'un nom de dépôt nomme
// une personne, et le publier reviendrait à publier la liste de classe. Un
// décompte dit ce qu'il faut savoir : qu'il y a de quoi comparer.
type Teaching struct {
	// Scope est la place du groupe : « a26.5n6.02 ».
	Scope string `json:"scope"`
	// Assignment est le nom court du travail : « tp1 ».
	Assignment string `json:"assignment"`
	// Teacher est le compte qui l'a donné, tel que le registre le connaît.
	Teacher string `json:"teacher"`
	// Copies compte les dépôts du travail. C'est le seul chiffre publié, et il
	// ne désigne personne.
	Copies int `json:"copies"`
	// LastHandin date la remise la plus récente, au jour près. Elle dit si le
	// travail est terminé, donc s'il vaut la peine d'être comparé.
	LastHandin string `json:"last_handin,omitempty"`
	// Indexed dit qu'un index d'empreintes est publié pour ce travail : sans
	// lui, un collègue ne peut que demander qu'on le publie.
	Indexed   bool   `json:"indexed,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

// ID désigne le travail sans ambiguïté : « a26.5n6.02.tp1 ».
func (t Teaching) ID() string {
	return t.Scope + naming.Separator + t.Assignment
}

// Key sert au rangement.
func (t Teaching) Key() string { return strings.ToLower(t.ID()) }

// GivenBy dit si le travail est celui de cet enseignant. Le navigateur et le
// terminal en tirent la même frontière : ses travaux se publient, ceux des
// autres se comparent.
func (t Teaching) GivenBy(teacher string) bool {
	return strings.EqualFold(t.Teacher, strings.TrimSpace(teacher))
}

// validate met une ligne en forme et refuse ce qui ne peut pas la désigner.
func (t Teaching) validate() (Teaching, error) {
	t.Scope = strings.ToLower(strings.TrimSpace(t.Scope))
	t.Assignment = strings.ToLower(strings.TrimSpace(t.Assignment))
	t.Teacher = strings.ToLower(strings.TrimSpace(t.Teacher))
	if _, _, ok := naming.SplitAssignment(t.ID()); !ok {
		return t, valid.Errorf(
			"Catalogue : « %s » n'est pas un travail de la nomenclature "+
				"(attendu : « a26.5n6.01.tp1 »).", t.ID())
	}
	if t.Teacher == "" {
		return t, valid.Errorf("Catalogue : « %s » ne dit pas qui l'a donné.", t.ID())
	}
	if t.Copies < 0 {
		return t, valid.Errorf("Catalogue : « %s » annonce %d copies.", t.ID(), t.Copies)
	}
	return t, nil
}

// Catalog est ce que l'organisation sait de ses travaux.
type Catalog struct {
	Version  int        `json:"version"`
	Teaching []Teaching `json:"teaching"`
}

// Empty dit qu'il n'y a rien à écrire.
func (c Catalog) Empty() bool { return len(c.Teaching) == 0 }

// Validate met le catalogue en forme.
func (c Catalog) Validate() (Catalog, error) {
	c.Version = Version
	vues := map[string]int{}
	lignes := make([]Teaching, 0, len(c.Teaching))
	for _, ligne := range c.Teaching {
		valide, err := ligne.validate()
		if err != nil {
			return c, err
		}
		// Un même travail écrit deux fois : le plus récent gagne. Deux postes
		// peuvent l'avoir publié, et l'un des deux a forcément écrit après.
		if rang, deja := vues[valide.Key()]; deja {
			if valide.UpdatedAt >= lignes[rang].UpdatedAt {
				lignes[rang] = valide
			}
			continue
		}
		vues[valide.Key()] = len(lignes)
		lignes = append(lignes, valide)
	}
	sort.Slice(lignes, func(first, second int) bool {
		return lignes[first].Key() < lignes[second].Key()
	})
	c.Teaching = lignes
	return c, nil
}

// With verse des lignes dans le catalogue et rend celui qui en résulte, avec un
// booléen qui dit s'il a bougé.
//
// Ce qui n'est pas nommé est laissé tel quel : publier ce qu'on donne ne doit
// pas retirer ce qu'un collègue a publié. C'est l'inverse des règles, qui se
// remplacent en bloc — mais le catalogue n'appartient à personne en particulier,
// et chacun n'y écrit que ses lignes.
func (c Catalog) With(entries []Teaching) (Catalog, bool, error) {
	if len(entries) == 0 {
		return c, false, nil
	}
	fusion := Catalog{Version: Version,
		Teaching: append([]Teaching(nil), c.Teaching...)}
	fusion.Teaching = append(fusion.Teaching, entries...)

	valide, err := fusion.Validate()
	if err != nil {
		return c, false, err
	}
	avant, err := EncodeCatalog(c)
	if err != nil {
		return c, false, err
	}
	apres, err := EncodeCatalog(valide)
	if err != nil {
		return c, false, err
	}
	return valide, string(avant) != string(apres), nil
}

// Of rend les travaux d'un enseignant, du plus récent au plus ancien.
func (c Catalog) Of(teacher string) []Teaching {
	siens := make([]Teaching, 0, 8)
	for _, ligne := range c.Teaching {
		if ligne.GivenBy(teacher) {
			siens = append(siens, ligne)
		}
	}
	byRecency(siens)
	return siens
}

// Others rend les travaux que les collègues d'un enseignant ont annoncés, du
// plus récent au plus ancien : ce à quoi il peut comparer ses copies quand
// l'index en est publié, et ce dont il peut demander la publication sinon.
func (c Catalog) Others(teacher string) []Teaching {
	autres := make([]Teaching, 0, 8)
	for _, ligne := range c.Teaching {
		if !ligne.GivenBy(teacher) {
			autres = append(autres, ligne)
		}
	}
	byRecency(autres)
	return autres
}

// Unindexed rend les travaux qu'un enseignant a annoncés sans en publier
// l'index, du plus récent au plus ancien.
//
// C'est ce qu'une passe automatisée a à faire, et la règle est ici plutôt que
// dans chaque interface : « manquant » doit vouloir dire la même chose au
// terminal, au navigateur et dans un workflow. Un collègue qui tombe sur l'un
// de ces travaux ne peut rien mesurer — il ne peut que demander qu'on le
// publie.
func (c Catalog) Unindexed(teacher string) []Teaching {
	manquants := make([]Teaching, 0, 4)
	for _, ligne := range c.Of(teacher) {
		if !ligne.Indexed {
			manquants = append(manquants, ligne)
		}
	}
	return manquants
}

// Course rend les travaux d'un cours, tous enseignants confondus. Les sigles
// équivalents sont attendus dépliés par l'appelant : c'est « rules » qui sait
// qu'un cours a changé de nom, pas le catalogue.
func (c Catalog) Course(codes []string) []Teaching {
	voulus := map[string]bool{}
	for _, code := range codes {
		voulus[strings.ToLower(strings.TrimSpace(code))] = true
	}
	trouves := make([]Teaching, 0, 8)
	for _, ligne := range c.Teaching {
		if parts, ok := naming.ParseTeam(ligne.ID()); ok && voulus[parts.Course] {
			trouves = append(trouves, ligne)
		}
	}
	byRecency(trouves)
	return trouves
}

// Find retrouve une ligne par l'identifiant de son travail.
func (c Catalog) Find(id string) (Teaching, bool) {
	id = strings.ToLower(strings.TrimSpace(id))
	for _, ligne := range c.Teaching {
		if ligne.Key() == id {
			return ligne, true
		}
	}
	return Teaching{}, false
}

// Teachers rend les comptes qui ont publié quelque chose, par nombre de travaux.
func (c Catalog) Teachers() []string {
	compte := map[string]int{}
	for _, ligne := range c.Teaching {
		compte[ligne.Teacher]++
	}
	comptes := make([]string, 0, len(compte))
	for compteur := range compte {
		comptes = append(comptes, compteur)
	}
	sort.Slice(comptes, func(first, second int) bool {
		if compte[comptes[first]] != compte[comptes[second]] {
			return compte[comptes[first]] > compte[comptes[second]]
		}
		return comptes[first] < comptes[second]
	})
	return comptes
}

// byRecency range des travaux de la session la plus récente à la plus ancienne.
func byRecency(entries []Teaching) {
	sort.SliceStable(entries, func(first, second int) bool {
		gauche := strings.SplitN(entries[first].Scope, naming.Separator, 2)
		droite := strings.SplitN(entries[second].Scope, naming.Separator, 2)
		if gauche[0] != droite[0] {
			return naming.CompareSessions(gauche[0], droite[0]) < 0
		}
		return entries[first].Key() < entries[second].Key()
	})
}

// DecodeCatalog relit un catalogue écrit.
func DecodeCatalog(content []byte) (Catalog, []string) {
	var lu Catalog
	if err := json.Unmarshal(content, &lu); err != nil {
		return Catalog{}, []string{fmt.Sprintf("Catalogue illisible : %v.", err)}
	}
	if lu.Version > Version {
		return Catalog{}, []string{fmt.Sprintf(
			"Catalogue : il vient d'une version %d de l'outil, qui n'en connaît que "+
				"%d. Mettez l'extension à jour (gh extension upgrade cohorte).",
			lu.Version, Version)}
	}
	// Une ligne mal écrite ne doit pas priver toute l'organisation de son
	// catalogue : elle est écartée et signalée, les autres restent.
	gardes := make([]Teaching, 0, len(lu.Teaching))
	soucis := make([]string, 0, 2)
	for _, ligne := range lu.Teaching {
		valide, err := ligne.validate()
		if err != nil {
			soucis = append(soucis, err.Error())
			continue
		}
		gardes = append(gardes, valide)
	}
	lu.Teaching = gardes
	valide, err := lu.Validate()
	if err != nil {
		return Catalog{}, append(soucis, err.Error())
	}
	return valide, soucis
}

// EncodeCatalog écrit un catalogue.
func EncodeCatalog(catalog Catalog) ([]byte, error) {
	valide, err := catalog.Validate()
	if err != nil {
		return nil, err
	}
	payload, err := json.MarshalIndent(valide, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(payload, '\n'), nil
}
