// Package registry tient le registre d'une organisation : ce que ni les noms de
// dépôts ni un poste ne peuvent dire — le nom complet en face de chaque compte
// GitHub, le rôle tenu (étudiant ou enseignant), et la date de remise de chaque
// travail —, rangé dans l'organisation elle-même plutôt que sur le poste de
// chacun.
//
// Une organisation nomme ses dépôts « session.cours.groupe.travail.étudiant »,
// et le dernier niveau est le nom slugifié, pas le compte. Rien dans les dépôts
// ne dit donc que « emilie-cote » est « @ecote » : c'est ce que le registre
// retient, et lui seul. Tant qu'il vivait sur un poste, un collègue ouvrant la
// même organisation n'y voyait que des slugs, et chaque groupe réécrivait les
// mêmes noms pour son compte — trois exemplaires d'une même personne, libres
// de diverger.
//
// Le registre tient dans un dépôt de service de l'organisation, en deux
// fichiers : les utilisateurs, et les dates de remise des travaux. Un seul
// commit les scelle, si bien qu'une lecture courante coûte une requête ; deux
// mille utilisateurs sur cinq ans pèsent quelques centaines de kilo-octets. Le
// découper davantage resterait possible : rien de ce qui suit ne dépend du
// nombre de fichiers.
//
// Le rôle qu'il porte nomme, il n'autorise pas. Savoir qu'un compte enseigne
// sert à le proposer, à le montrer, à chercher les cours qu'il a donnés ; ce
// qui décide réellement de ce que quelqu'un peut lire, ce sont les droits
// GitHub — appartenance à l'organisation, équipes, collaborateurs. Un étudiant
// ne peut pas se déclarer enseignant, non parce que ce fichier le lui refuse,
// mais parce qu'il n'a jamais eu le droit d'y écrire.
package registry

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/PierreOlivierBrillant/gh-milou/internal/brand"
	"github.com/PierreOlivierBrillant/gh-milou/internal/exchange"
	"github.com/PierreOlivierBrillant/gh-milou/internal/naming"
	"github.com/PierreOlivierBrillant/gh-milou/internal/roster"
	"github.com/PierreOlivierBrillant/gh-milou/internal/rules"
	"github.com/PierreOlivierBrillant/gh-milou/internal/signature"
	"github.com/PierreOlivierBrillant/gh-milou/internal/valid"
)

// Emplacement du registre dans l'organisation.
const (
	// RepoName est le dépôt de service qui le porte. Le point de tête le range
	// avec « .github », le signale comme dépôt de service, et le met hors
	// d'atteinte de la nomenclature : un nom à cinq niveaux ne peut pas
	// commencer par un niveau vide. Il garde le nom d'avant Milou : le
	// changer obligerait chaque organisation déjà amorcée à une migration,
	// pour un mot que personne ne tape.
	RepoName = ".cohorte"
	// Branch est la seule branche écrite.
	Branch = "main"
	// UsersFile porte les utilisateurs. Il garde son nom d'origine bien qu'il
	// porte désormais aussi les enseignants : le renommer obligerait chaque
	// organisation déjà amorcée à une migration, pour un mot.
	UsersFile = "etudiants.json"
	// AssignmentsFile porte les dates de remise. Il est à part plutôt que dans
	// le même fichier : les utilisateurs et les travaux ne changent ni au même
	// rythme ni sous la même main, et deux fichiers rendent lisible sur
	// github.com ce qu'un commit a vraiment touché.
	AssignmentsFile = "travaux.json"
	// RulesFile porte ce que l'équipe déclare pour la comparaison des copies :
	// les sigles qui désignent le même cours au fil des ans, les noms qui
	// désignent le même travail, les profils d'inspection maison. Il est à
	// part parce qu'il ne parle de personne : le modifier n'engage aucun
	// renseignement personnel, et il peut se relire sur github.com sans
	// exposer une liste de noms.
	RulesFile = "regles.json"
	// AsksFile porte les demandes de levée du voile. Elles ne nomment aucun
	// étudiant : un travail, un jeton, et ce que le demandeur a mesuré.
	AsksFile = exchange.AsksFile
	// MarksFile porte les marques invisibles délivrées. C'est le seul fichier
	// du registre qui relie une marque à quelqu'un : sans lui, deux travaux qui
	// portent la même se reconnaissent encore, mais personne ne peut dire de
	// qui il s'agit.
	MarksFile = signature.BookFile
	// CatalogFile porte le catalogue des travaux donnés : la place, le nom du
	// travail, un décompte. Il est à part parce qu'il ne nomme personne — un
	// collègue peut le lire sans qu'aucune liste de classe ne lui soit ouverte.
	CatalogFile = exchange.CatalogFile
	// ReadmeFile explique le dépôt à qui l'ouvre sur github.com. C'est bien
	// « README.md » : GitHub n'affiche que celui-là sur la page du dépôt, et
	// c'est aussi le fichier que la création avec « auto_init » y dépose — le
	// nôtre prend sa place plutôt que de s'ajouter à côté.
	ReadmeFile = "README.md"
)

// Version est celle du schéma écrit. Elle est relue, jamais devinée : un
// fichier venu d'une version ultérieure de l'outil doit pouvoir se signaler.
//
// La version 2 ajoute le rôle et range les fiches sous « users ». Une version 1
// se relit telle quelle — sans rôle, tout le monde est étudiant —, et se
// réécrit en version 2 à la première écriture.
//
// La version 3 ajoute le renvoi d'un compte à un autre de la même personne
// (« same_as »). Elle n'est écrite que lorsqu'un renvoi existe : un poste resté
// en version 2 relit sans perte un registre qui n'en porte aucun, et l'avertir
// pour rien lui apprendrait à ignorer l'avertissement. Quand il y en a un, en
// revanche, l'avertissement est mérité : ce poste-là montrerait deux personnes,
// et réécrirait le fichier sans le renvoi.
const Version = 3

// plainVersion est celle qu'on écrit quand rien n'exige la suivante.
const plainVersion = 2

// User est une personne connue de l'organisation : un étudiant, ou quelqu'un
// qui enseigne.
type User struct {
	Username string `json:"username"`
	FullName string `json:"full_name"`
	// IsTeacher dit que cette personne enseigne. Il est écrit pour tout le
	// monde, « false » compris : un champ absent se lirait « on ne sait pas »,
	// alors qu'on sait — le registre est la liste de ce qu'on sait.
	//
	// Ce n'est pas un droit, c'est une déclaration. Personne n'accède à quoi
	// que ce soit parce que ce champ vaut « true » ; c'est l'inverse — seul
	// quelqu'un qui a déjà le droit d'écrire ici peut le mettre à « true ».
	IsTeacher bool `json:"is_teacher"`
	// StudentID est le matricule du collège. C'est lui qui identifie vraiment
	// quelqu'un : deux comptes qui le portent sont la même personne, et deux
	// personnes du même nom ne le partagent pas. Le registre le retient pour
	// que ce soit vrai d'un poste à l'autre.
	//
	// Un enseignant n'en a pas : il n'est pas inscrit au collège. Son compte
	// GitHub suffit à le désigner.
	StudentID string `json:"student_id,omitempty"`
	// Slugs énumère tout ce qui a désigné cette personne au dernier niveau d'un
	// nom de dépôt. La liste s'allonge, ne se raccourcit pas : corriger
	// l'orthographe d'un nom ne doit pas rendre orphelins les dépôts déjà
	// créés sous l'ancien slug.
	//
	// C'est la différence avec ce que l'outil faisait jusqu'ici, où le
	// rapprochement slug → personne était reconstruit à la lecture depuis le
	// nom courant et quelques heuristiques. Ici il est déclaré.
	Slugs   []string `json:"slugs,omitempty"`
	AddedAt string   `json:"added_at,omitempty"`
	// SameAs renvoie à l'autre compte de la même personne : celui qui la
	// désigne, et dont le nom et le matricule l'emportent. Vide, la fiche est
	// la sienne propre.
	//
	// C'est un renvoi plutôt qu'une fusion des deux fiches. Fondre l'une dans
	// l'autre perdrait ce que celle qu'on retire savait — son nom, ses slugs,
	// sa date d'ajout —, et la fusion ne pourrait plus se défaire : or une
	// fusion erronée réunit deux vraies personnes sous un seul dépôt, ce que
	// le refus des homonymes protège justement. Le renvoi se retire, et chaque
	// fiche retrouve alors exactement ce qu'elle portait.
	//
	// Rien ne l'écrit qu'une décision : « Join » et « Split ». Un nom ne se
	// rapproche jamais d'un autre tout seul.
	SameAs string `json:"same_as,omitempty"`
}

// Key sert au rangement : le compte GitHub est insensible à la casse.
func (u User) Key() string { return strings.ToLower(strings.TrimSpace(u.Username)) }

// Person rend la personne telle que le reste de l'outil la manipule.
func (u User) Person() roster.Person {
	return roster.Person{
		FullName: u.FullName, Username: u.Username, StudentID: u.StudentID,
	}
}

// Role nomme ce que la personne est, tel que les trois interfaces l'écrivent.
// Le mot est décidé ici pour qu'il soit le même partout.
func (u User) Role() string {
	if u.IsTeacher {
		return RoleTeacher
	}
	return RoleStudent
}

// Les deux rôles. « Utilisateur » n'en est pas un : c'est le mot qui les
// rassemble, et il ne s'écrit jamais en face de quelqu'un.
const (
	RoleStudent = "étudiant"
	RoleTeacher = "enseignant"
)

// From compose la fiche d'une personne. Le slug que son nom complet produit y
// est joint d'emblée : c'est celui que porteront ses dépôts, et le retenir
// maintenant évite d'avoir à le deviner plus tard.
func From(person roster.Person) User {
	fiche := User{
		Username: person.Username, FullName: person.FullName,
		StudentID: person.StudentID,
	}
	if slug, err := naming.Student(person.FullName); err == nil {
		fiche.Slugs = []string{slug}
	}
	return fiche
}

// validate met une fiche en forme et refuse ce qui ne peut désigner personne.
// Le nom complet, lui, peut manquer : un compte adopté depuis des dépôts
// hérités n'a pas encore le sien.
func (u User) validate() (User, error) {
	username, err := valid.Login(u.Username, "Compte GitHub")
	if err != nil {
		return u, err
	}
	u.Username = username
	if nom := strings.TrimSpace(u.FullName); nom != "" {
		if u.FullName, err = valid.FullName(nom); err != nil {
			return u, err
		}
	} else {
		u.FullName = ""
	}
	u.StudentID = strings.TrimSpace(u.StudentID)
	u.Slugs = cleanSlugs(u.Slugs)
	// Un renvoi illisible est retiré plutôt que de faire écarter la fiche :
	// une faute de frappe dans « same_as » ne doit pas priver la personne de
	// son nom. « Decode » le signale.
	u.SameAs = strings.TrimSpace(u.SameAs)
	if u.SameAs != "" {
		autre, err := valid.Login(u.SameAs, "Même personne que")
		if err != nil || strings.EqualFold(autre, u.Username) {
			autre = ""
		}
		u.SameAs = autre
	}
	return u, nil
}

// cleanSlugs met les slugs en forme, les dédoublonne et les range. L'ordre est
// fixe pour que deux écritures du même registre donnent le même fichier.
func cleanSlugs(slugs []string) []string {
	vus := map[string]bool{}
	propres := make([]string, 0, len(slugs))
	for _, slug := range slugs {
		propre := strings.ToLower(strings.TrimSpace(slug))
		if propre == "" || vus[propre] {
			continue
		}
		vus[propre] = true
		propres = append(propres, propre)
	}
	sort.Strings(propres)
	if len(propres) == 0 {
		return nil
	}
	return propres
}

// ------------------------------------------------------------------ registre

// Set est le registre en mémoire. Il ne se modifie pas : chaque changement en
// produit un nouveau, si bien qu'une écriture refusée peut être rejouée sans
// craindre d'avoir déjà entamé celui qu'on avait en main.
type Set struct {
	users   []User // rangés par compte, casse ignorée
	byLogin map[string]int
	bySlug  map[string]int
	// roots donne, pour chaque fiche, la position de celle qui désigne sa
	// personne ; others, pour chacune de celles-là, les fiches qui y renvoient.
	// Une personne à un seul compte est sa propre racine, sans autres.
	roots  []int
	others map[int][]int
	// Le registre a deux sections, dans deux fichiers : les utilisateurs, et
	// les dates de remise. Elles sont tenues ensemble parce qu'un seul commit
	// les scelle — ce qu'on a lu de l'une vaut aussi longtemps que l'autre.
	assignments  []Assignment
	byAssignment map[string]int
	// Les règles que l'équipe déclare : équivalences de sigles, profils
	// d'inspection, bornes préférées. Elles voyagent avec le reste parce qu'un
	// seul commit scelle les fichiers.
	rules rules.Rules
	// Le catalogue des travaux donnés : la place, le nom, un décompte. Il ne
	// nomme aucun étudiant, et c'est lui qui permet à un enseignant de voir ce
	// qu'un collègue a donné sans voir ses dépôts.
	catalog exchange.Catalog
	// Les marques invisibles délivrées : un jeton par personne et par travail.
	// C'est le seul endroit qui relie une marque à quelqu'un — deux travaux qui
	// portent la même se reconnaissent sans lui, mais nul autre ne peut dire
	// de qui il s'agit.
	marks signature.Book
	// Les demandes de levée du voile. Elles ne nomment aucun étudiant : un
	// travail, un jeton, et ce que le demandeur a mesuré.
	asks exchange.Asks
}

// newSet range les fiches et dresse ses index.
func newSet(users []User, assignments []Assignment, declared rules.Rules,
	catalog exchange.Catalog, marks signature.Book, asks exchange.Asks) *Set {
	rangees := append([]User(nil), users...)
	sort.SliceStable(rangees, func(i, j int) bool {
		return rangees[i].Key() < rangees[j].Key()
	})
	travaux := append([]Assignment(nil), assignments...)
	sort.SliceStable(travaux, func(i, j int) bool {
		return travaux[i].Key() < travaux[j].Key()
	})
	set := &Set{
		rules:        declared,
		catalog:      catalog,
		marks:        marks,
		asks:         asks,
		users:        rangees,
		byLogin:      make(map[string]int, len(rangees)),
		bySlug:       make(map[string]int, 2*len(rangees)),
		assignments:  travaux,
		byAssignment: make(map[string]int, len(travaux)),
	}
	for position, travail := range travaux {
		set.byAssignment[travail.Key()] = position
	}
	for position, fiche := range rangees {
		set.byLogin[fiche.Key()] = position
		// Le compte désigne aussi la personne : les dépôts adoptés d'une
		// organisation sans convention le portent souvent à la place du nom.
		set.bySlug[fiche.Key()] = position
		for _, slug := range fiche.Slugs {
			set.bySlug[slug] = position
		}
	}
	set.roots = rootsOf(rangees)
	set.others = map[int][]int{}
	for position, racine := range set.roots {
		if racine != position {
			set.others[racine] = append(set.others[racine], position)
		}
	}
	return set
}

// rootsOf rend, pour chaque fiche, la position de celle qui désigne sa
// personne.
//
// Les renvois s'écrivent toujours vers la fiche qui désigne, si bien qu'un seul
// saut suffit d'ordinaire. Le fichier se modifie pourtant à la main : une
// chaîne se suit jusqu'au bout, un renvoi vers un compte inconnu se lit comme
// absent, et une boucle désigne la première de ses fiches — la même, d'où
// qu'on y entre, sans quoi deux comptes de la boucle se croiraient chacun la
// personne entière.
func rootsOf(fiches []User) []int {
	position := make(map[string]int, len(fiches))
	for index, fiche := range fiches {
		position[fiche.Key()] = index
	}
	racines := make([]int, len(fiches))
	for index := range fiches {
		chemin := []int{index}
		courant := index
		for {
			cible, connue := position[strings.ToLower(fiches[courant].SameAs)]
			if fiches[courant].SameAs == "" || !connue {
				break
			}
			if deja := slices.Index(chemin, cible); deja >= 0 {
				courant = slices.Min(chemin[deja:])
				break
			}
			chemin = append(chemin, cible)
			courant = cible
		}
		racines[index] = courant
	}
	return racines
}

// Empty rend un registre vide : celui d'une organisation qu'on n'a pas encore
// amorcée.
func Empty() *Set {
	return newSet(nil, nil, rules.Rules{}, exchange.Catalog{}, signature.Book{},
		exchange.Asks{})
}

// Rules rend ce que l'organisation déclare.
func (s *Set) Rules() rules.Rules { return s.rules }

// Asks rend les demandes de levée du voile.
func (s *Set) Asks() exchange.Asks {
	if s == nil {
		return exchange.Asks{}
	}
	return s.asks
}

// Marks rend les marques invisibles délivrées.
func (s *Set) Marks() signature.Book {
	if s == nil {
		return signature.Book{}
	}
	return s.marks
}

// Catalog rend ce que l'organisation sait des travaux donnés.
func (s *Set) Catalog() exchange.Catalog {
	if s == nil {
		return exchange.Catalog{}
	}
	return s.catalog
}

// NameFor rend le nom complet derrière un dépôt de la nomenclature.
//
// À défaut, c'est le fragment qui nomme la personne — « ancien-eleve » — et non
// le nom du dépôt entier. La différence compte à l'écran : une liste de paires
// où l'on lit « Émilie Côté » d'un côté et « h24.5m6.02.tp-1.ancien-eleve » de
// l'autre est une liste qu'il faut déchiffrer ligne à ligne.
func (s *Set) NameFor(repoName string) string {
	parts, reconnu := naming.Parse(repoName)
	if !reconnu {
		return repoName
	}
	if s != nil {
		if user, connu := s.Resolve(parts.Student); connu && user.FullName != "" {
			return user.FullName
		}
	}
	return parts.Student
}

// Len compte les personnes connues.
func (s *Set) Len() int { return len(s.users) }

// All rend les fiches, rangées par compte. La copie évite qu'un appelant
// modifie le registre dans son dos.
func (s *Set) All() []User { return append([]User(nil), s.users...) }

// Assignments rend les travaux datés, rangés par identifiant. La copie évite
// qu'un appelant modifie le registre dans son dos.
func (s *Set) Assignments() []Assignment {
	return append([]Assignment(nil), s.assignments...)
}

// Due rend la date cible d'un travail, ou une chaîne vide s'il n'en a pas.
//
// C'est la seconde question que le registre permet de poser, et « classroom »
// ne lui en pose pas d'autre : quand ce travail est-il attendu ?
func (s *Set) Due(assignmentID string) string {
	position, connu := s.byAssignment[strings.ToLower(strings.TrimSpace(assignmentID))]
	if !connu {
		return ""
	}
	return s.assignments[position].Due
}

// Find retrouve une personne par son compte GitHub.
func (s *Set) Find(username string) (User, bool) {
	position, connu := s.byLogin[strings.ToLower(strings.TrimSpace(username))]
	if !connu {
		return User{}, false
	}
	return s.users[position], true
}

// PersonOf rend la personne entière derrière un compte : celui qui la désigne,
// avec ce que ses autres comptes savent d'elle.
//
// Ce qui l'emporte est dit ici, une fois. Le nom complet et le matricule sont
// ceux du compte qui la désigne, et ceux d'un autre de ses comptes à défaut : le
// renvoi a été écrit vers ce compte-là, c'est lui qu'on a choisi de garder. Le
// rôle, lui, est celui du plus haut : si l'un de ses comptes enseigne, elle
// enseigne. Les slugs s'additionnent — chacun rattache des dépôts déjà créés.
func (s *Set) PersonOf(username string) (User, bool) {
	position, connu := s.byLogin[strings.ToLower(strings.TrimSpace(username))]
	if !connu {
		return User{}, false
	}
	return s.whole(position), true
}

// whole compose la personne entière à partir de l'une de ses fiches.
func (s *Set) whole(position int) User {
	racine := s.roots[position]
	fiche := s.users[racine]
	fiche.SameAs = ""
	for _, autre := range s.others[racine] {
		suivante := s.users[autre]
		if strings.TrimSpace(fiche.FullName) == "" {
			fiche.FullName = suivante.FullName
		}
		if fiche.StudentID == "" {
			fiche.StudentID = suivante.StudentID
		}
		fiche.IsTeacher = fiche.IsTeacher || suivante.IsTeacher
		fiche.Slugs = cleanSlugs(append(append([]string(nil), fiche.Slugs...),
			suivante.Slugs...))
	}
	return fiche
}

// Accounts rend tous les comptes de la personne derrière un compte : celui qui
// la désigne d'abord, puis les autres par ordre alphabétique. Un compte que le
// registre ignore n'en a aucun.
func (s *Set) Accounts(username string) []string {
	position, connu := s.byLogin[strings.ToLower(strings.TrimSpace(username))]
	if !connu {
		return nil
	}
	racine := s.roots[position]
	comptes := []string{s.users[racine].Username}
	for _, autre := range s.others[racine] {
		comptes = append(comptes, s.users[autre].Username)
	}
	return comptes
}

// Teachers rend ceux qui enseignent, rangés par compte : une fois chacun, sous
// le compte qui le désigne, même s'il en a plusieurs. C'est ce qui permet de
// chercher les cours qu'un collègue a déjà donnés, et de proposer un nom quand
// il faut désigner l'enseignant d'un groupe.
func (s *Set) Teachers() []User {
	enseignants := make([]User, 0)
	for position := range s.users {
		if s.roots[position] != position {
			continue
		}
		if personne := s.whole(position); personne.IsTeacher {
			enseignants = append(enseignants, personne)
		}
	}
	return enseignants
}

// Knows dit si le registre connaît un compte. Un compte qu'il ignore a pu
// laisser des dépôts sans que personne ne l'ait jamais nommé.
func (s *Set) Knows(username string) bool {
	_, connu := s.Find(username)
	return connu
}

// Teaches dit si un compte est déclaré enseignant — lui, ou un autre compte de
// la même personne. Un compte inconnu ne l'est pas : le registre est la liste
// de ce qu'on sait, et ce qu'il ignore n'enseigne pas.
func (s *Set) Teaches(username string) bool {
	personne, connue := s.PersonOf(username)
	return connue && personne.IsTeacher
}

// Name rend le nom complet de la personne derrière un compte, ou une chaîne
// vide s'il est inconnu.
func (s *Set) Name(username string) string {
	personne, connue := s.PersonOf(username)
	if !connue {
		return ""
	}
	return personne.FullName
}

// Resolve retrouve à qui appartient le dernier niveau d'un nom de dépôt : la
// personne entière, sous le compte qui la désigne. Les slugs de chacun de ses
// comptes y mènent, si bien qu'un dépôt créé sous l'un ou l'autre nom reste le
// sien.
//
// La marque que GitHub ajoute à un nom déjà pris — « -1 », puis « -2 » — n'en
// fait pas quelqu'un d'autre : « emilie-cote-1 » est « emilie-cote ». Elle
// n'est retirée qu'en dernier recours, car un vrai slug peut se terminer
// pareil.
func (s *Set) Resolve(slug string) (User, bool) {
	fragment := strings.ToLower(strings.TrimSpace(slug))
	// Un fragment vide ne désigne personne. La question se pose depuis qu'une
	// liste peut porter quelqu'un dont on ne connaît pas encore le compte : le
	// prendre pour une clé reviendrait à lui attribuer la première fiche venue,
	// et à effacer le seul nom qu'on ait de lui.
	if fragment == "" {
		return User{}, false
	}
	if position, connu := s.bySlug[fragment]; connu {
		return s.whole(position), true
	}
	base, marque := roster.WithoutDuplicateMarker(fragment)
	if !marque {
		return User{}, false
	}
	position, connu := s.bySlug[strings.ToLower(base)]
	if !connu {
		return User{}, false
	}
	return s.whole(position), true
}

// Lookup répond à la question que « classroom » pose au registre : qui se
// cache derrière le dernier niveau d'un nom de dépôt ?
//
// Une fiche sans nom complet répond quand même. Le compte GitHub est déjà une
// réponse : il dit que ce dépôt est celui de quelqu'un qu'on connaît, et non
// d'un slug orphelin. Refuser de le dire rendait invisibles — donc
// innommables et indéplaçables — les personnes qu'on n'a jamais eu l'occasion
// de nommer, celles des dépôts repris qui portent leur compte.
//
// La personne rendue porte tous ses comptes : c'est par là que « classroom »
// apprend qu'un étudiant en a un autre, et qu'il l'invite, le reconnaît dans
// une remise et lui attribue ses dépôts sous l'un comme sous l'autre.
func (s *Set) Lookup(fragment string) (roster.Person, bool) {
	fiche, trouve := s.Resolve(fragment)
	if !trouve {
		return roster.Person{}, false
	}
	personne := fiche.Person()
	if comptes := s.Accounts(fiche.Username); len(comptes) > 1 {
		personne.Also = comptes[1:]
	}
	return personne, true
}

// ---------------------------------------------------------------- changement

// Change est ce qu'une écriture veut faire au registre.
//
// Elle décrit une intention — « ces personnes sont connues » —, non un état.
// C'est ce qui permet de la rejouer telle quelle sur un registre fraîchement
// relu quand quelqu'un a écrit entre-temps : le travail de l'autre est
// conservé entier, et rien n'est jamais fusionné.
type Change struct {
	// Learn fait connaître des personnes, ou complète ce qu'on sait d'elles.
	Learn []User
	// Forget retire des personnes, par compte. C'est rare et lourd de
	// conséquences : un compte oublié laisse ses dépôts sans nom.
	Forget []string
	// Roles change le rôle de comptes déjà connus.
	//
	// Il est à part de « Learn » exprès. Apprendre quelqu'un ne dit rien de
	// son rôle : une liste de classe importée ne sait pas qui enseigne, et si
	// elle pouvait l'écrire, réimporter la liste d'un groupe ferait
	// redescendre étudiant l'enseignant qui s'y trouve. Le rôle ne change donc
	// que lorsqu'on l'a demandé, et pour les comptes qu'on a nommés.
	Roles []RoleChange
	// Links réunit des comptes d'une même personne, ou les sépare.
	//
	// Il est à part de « Learn » pour la même raison que le rôle : apprendre
	// quelqu'un ne dit rien de ses autres comptes, et réimporter une liste ne
	// doit ni réunir deux personnes ni défaire ce qu'une décision a réuni.
	Links []Link
	// Deadlines fixe la date cible de travaux. Une date vide la retire : il
	// n'y a pas de geste séparé pour cela, c'est la même décision prise dans
	// l'autre sens.
	Deadlines []Assignment
	// Rules remplace les règles de l'organisation. Nil n'y touche pas — c'est
	// la différence entre « je ne déclare rien » et « je retire tout ».
	Rules *rules.Rules
	// Asks verse des demandes de levée du voile, ou les tranche : une demande
	// réécrite avec le même identifiant remplace la sienne.
	Asks []exchange.Ask
	// Marks verse des marques invisibles au registre. Comme le catalogue, il ne
	// remplace pas : une marque délivrée à quelqu'un d'autre n'a pas à
	// disparaître parce qu'on redistribue un travail.
	Marks []signature.Issued
	// Teaching verse des lignes au catalogue des travaux. Contrairement aux
	// règles, il ne remplace pas : chacun n'y écrit que ses lignes, et publier
	// les siennes ne doit pas retirer celles d'un collègue.
	Teaching []exchange.Teaching
	// Reason est ce que dira le message de commit. Vide, il est composé.
	Reason string
}

// RoleChange dit ce que devient le rôle d'un compte.
type RoleChange struct {
	Username  string `json:"username"`
	IsTeacher bool   `json:"is_teacher"`
}

// Link dit à quel compte un autre renvoie : « Username » est la même personne
// que « SameAs ». Un « SameAs » vide l'en sépare.
type Link struct {
	Username string `json:"username"`
	SameAs   string `json:"same_as"`
}

// Learn compose le changement qui fait connaître des personnes.
func Learn(people ...roster.Person) Change {
	fiches := make([]User, 0, len(people))
	for _, person := range people {
		fiches = append(fiches, From(person))
	}
	return Change{Learn: fiches}
}

// Name compose le changement qui donne son nom complet à un compte, ou le
// corrige. Il ne renomme aucun dépôt.
//
// Les noms que des listes de groupe lui donnaient y laissent leur slug. Le
// registre ne les a peut-être jamais reçus, et ce sont pourtant eux qui ont
// nommé ses dépôts : le nom corrigé ne doit pas les détacher de lui.
func Name(person roster.Person, elsewhere ...roster.Person) Change {
	fiche := From(person)
	for _, ailleurs := range elsewhere {
		if !ailleurs.Owns(person.Username) {
			continue
		}
		if slug, err := naming.Student(ailleurs.FullName); err == nil {
			fiche.Slugs = append(fiche.Slugs, slug)
		}
	}
	change := Change{Learn: []User{fiche}}
	if nom := strings.TrimSpace(person.FullName); nom != "" {
		change.Reason = "Nomme @" + strings.TrimSpace(person.Username) + " : " + nom
	}
	return change
}

// LearnSlug retient qu'un slug désigne un compte. C'est ce qu'on apprend en
// adoptant des dépôts déjà nommés autrement que par le nom complet.
func LearnSlug(username, slug string) Change {
	return Change{Learn: []User{{Username: username, Slugs: []string{slug}}}}
}

// SetRole compose le changement qui fait d'un compte un enseignant, ou l'en
// défait. C'est la cooptation : quelqu'un qui enseigne déjà reconnaît que
// quelqu'un d'autre enseigne aussi.
func SetRole(username string, teacher bool) Change {
	verbe := "Retire le rôle d'enseignant à @"
	if teacher {
		verbe = "Reconnaît @"
	}
	suite := " comme enseignant"
	if !teacher {
		suite = ""
	}
	return Change{
		Roles:  []RoleChange{{Username: username, IsTeacher: teacher}},
		Reason: verbe + strings.TrimSpace(username) + suite,
	}
}

// Join compose le changement qui fait d'un compte la même personne qu'un autre.
// C'est « principal » qui la désigne ensuite : son nom complet et son matricule
// l'emportent, et c'est sous lui que l'annuaire la montre.
//
// Rien n'est retiré ni renommé. Les deux fiches restent entières, l'une
// renvoyant à l'autre ; les dépôts créés sous l'un ou l'autre nom restent les
// siens, et aucune liste de groupe n'est touchée.
func Join(account, principal string) Change {
	compte, autre := strings.TrimSpace(account), strings.TrimSpace(principal)
	return Change{
		Links:  []Link{{Username: compte, SameAs: autre}},
		Reason: "Réunit @" + compte + " à @" + autre + " : une même personne",
	}
}

// Split compose le changement qui défait une réunion : le compte redevient une
// personne à lui seul, avec exactement ce que sa fiche portait. Les autres
// comptes de la personne restent ensemble.
func Split(account string) Change {
	compte := strings.TrimSpace(account)
	return Change{
		Links:  []Link{{Username: compte}},
		Reason: "Sépare @" + compte + " des autres comptes de sa personne",
	}
}

// Schedule compose le changement qui fixe la date cible d'un travail, désigné
// par son identifiant complet — « a26.5n6.01.tp1 ». Une date vide la retire.
func Schedule(assignmentID, due string) Change {
	return Change{Deadlines: []Assignment{{ID: assignmentID, Due: due}}}
}

// Reschedule compose le changement qui fixe plusieurs échéances d'un coup.
// C'est ce que demande un travail renommé ou déplacé : son échéance quitte
// l'identifiant qu'il avait pour celui qu'il prend, et les deux mouvements ne
// doivent pas pouvoir se séparer.
func Reschedule(travaux ...Assignment) Change {
	return Change{Deadlines: append([]Assignment(nil), travaux...)}
}

// Empty dit qu'il n'y a rien à écrire.
func (c Change) Empty() bool {
	return len(c.Learn) == 0 && len(c.Forget) == 0 && len(c.Links) == 0 &&
		len(c.Roles) == 0 && len(c.Deadlines) == 0 && c.Rules == nil &&
		len(c.Teaching) == 0 && len(c.Marks) == 0 && len(c.Asks) == 0
}

// Declare compose le changement qui remplace les règles de l'organisation.
func Declare(declared rules.Rules) Change {
	return Change{Rules: &declared, Reason: "Règles de comparaison"}
}

// AskFor compose le changement qui dépose ou tranche des demandes.
func AskFor(asks ...exchange.Ask) Change {
	return Change{Asks: asks, Reason: "Demandes de comparaison"}
}

// Mark compose le changement qui verse des marques invisibles.
func Mark(issued ...signature.Issued) Change {
	return Change{Marks: issued, Reason: "Signatures délivrées"}
}

// Publish compose le changement qui verse des travaux au catalogue.
func Publish(entries ...exchange.Teaching) Change {
	return Change{Teaching: entries, Reason: "Travaux donnés"}
}

// With applique un changement et rend le registre qui en résulte, avec un
// booléen qui dit s'il a bougé. Appliqué deux fois, le même changement donne
// le même résultat : c'est ce qui rend le rejeu sûr.
//
// La date est fournie par l'appelant plutôt que lue à l'horloge : celle du
// magasin quand il écrit, une date fixe quand un test veut deux exécutions
// identiques.
func (s *Set) With(change Change, today string) (*Set, bool, error) {
	fiches := s.All()
	position := make(map[string]int, len(fiches))
	for index, fiche := range fiches {
		position[fiche.Key()] = index
	}

	bouge := false
	for _, appris := range change.Learn {
		valide, err := appris.validate()
		if err != nil {
			return nil, false, err
		}
		index, connu := position[valide.Key()]
		if !connu {
			valide.AddedAt = today
			// Seul « Links » écrit un renvoi : une fiche apprise n'en apporte pas.
			valide.SameAs = ""
			position[valide.Key()] = len(fiches)
			fiches = append(fiches, valide)
			bouge = true
			continue
		}
		fondu := merge(fiches[index], valide)
		if !same(fiches[index], fondu) {
			fiches[index] = fondu
			bouge = true
		}
	}

	// Les renvois se posent après l'apprentissage, pour la même raison que le
	// rôle, et avant lui : retirer le rôle doit atteindre tous les comptes
	// qu'on vient de réunir.
	for _, lien := range change.Links {
		relies, relieOnt, err := linked(fiches, lien, today)
		if err != nil {
			return nil, false, err
		}
		fiches, bouge = relies, bouge || relieOnt
	}
	for index, fiche := range fiches {
		position[fiche.Key()] = index
	}

	// Le rôle se pose après l'apprentissage : coopter quelqu'un qu'on vient
	// d'apprendre doit marcher en une seule écriture.
	for _, role := range change.Roles {
		compte, err := valid.Login(role.Username, "Compte GitHub")
		if err != nil {
			return nil, false, err
		}
		index, connu := position[strings.ToLower(compte)]
		if !connu {
			return nil, false, valid.Errorf(
				"Le registre ne connaît pas @%s : son rôle ne peut pas être changé "+
					"tant qu'il n'y figure pas.", compte)
		}
		// Reconnaître quelqu'un se pose sur le compte nommé : il suffit que
		// l'un des siens enseigne. Le lui retirer, en revanche, doit atteindre
		// tous ses comptes — sans quoi un autre continuerait d'enseigner pour
		// lui, et le geste n'aurait rien fait.
		touchees := []int{index}
		if !role.IsTeacher {
			touchees = samePerson(fiches, index)
		}
		for _, touchee := range touchees {
			if fiches[touchee].IsTeacher == role.IsTeacher {
				continue
			}
			fiches[touchee].IsTeacher = role.IsTeacher
			bouge = true
		}
	}

	if len(change.Forget) > 0 {
		oublies := map[string]bool{}
		for _, username := range change.Forget {
			oublies[strings.ToLower(strings.TrimSpace(username))] = true
			// Oublier le compte qui désigne une personne ne doit pas séparer
			// les autres : ils restent ensemble, sous l'un d'eux.
			fiches = unlinked(fiches, strings.TrimSpace(username))
		}
		restantes := make([]User, 0, len(fiches))
		for _, fiche := range fiches {
			if oublies[fiche.Key()] {
				bouge = true
				continue
			}
			restantes = append(restantes, fiche)
		}
		fiches = restantes
	}

	travaux, datesOnt, err := s.scheduled(change.Deadlines, today)
	if err != nil {
		return nil, false, err
	}

	catalogue, catalogueOnt, err := s.catalog.With(change.Teaching)
	if err != nil {
		return nil, false, err
	}
	marques, marquesOnt, err := s.marks.With(change.Marks)
	if err != nil {
		return nil, false, err
	}
	demandes, demandesOnt, err := s.asks.With(change.Asks)
	if err != nil {
		return nil, false, err
	}

	// Les règles se remplacent en bloc plutôt que de se fusionner : ce qu'on
	// déclare est la liste complète des équivalences, et en retirer une doit
	// pouvoir se faire. Les fusionner rendrait tout ajout définitif.
	declarees, regleOnt := s.rules, false
	if change.Rules != nil {
		valides, err := change.Rules.Validate()
		if err != nil {
			return nil, false, err
		}
		avant, err := rules.Encode(s.rules)
		if err != nil {
			return nil, false, err
		}
		apres, err := rules.Encode(valides)
		if err != nil {
			return nil, false, err
		}
		declarees, regleOnt = valides, !bytes.Equal(avant, apres)
	}
	return newSet(fiches, travaux, declarees, catalogue, marques, demandes),
		bouge || datesOnt || regleOnt || catalogueOnt || marquesOnt || demandesOnt, nil
}

// scheduled applique à la section des échéances ce qu'un changement lui
// demande, et rend les travaux datés qui en résultent.
func (s *Set) scheduled(demandes []Assignment, today string) ([]Assignment, bool, error) {
	if len(demandes) == 0 {
		return s.assignments, false, nil
	}
	travaux := s.Assignments()
	position := make(map[string]int, len(travaux))
	for index, travail := range travaux {
		position[travail.Key()] = index
	}

	bouge := false
	retires := map[string]bool{}
	for _, demande := range demandes {
		valide, err := demande.validate()
		if err != nil {
			return nil, false, err
		}
		index, connu := position[valide.Key()]
		if valide.Due == "" {
			if connu && !retires[valide.Key()] {
				retires[valide.Key()] = true
				bouge = true
			}
			continue
		}
		// Fixer une date sur un travail qu'on vient de retirer dans le même
		// changement le remet : c'est ce que fait un travail renommé.
		delete(retires, valide.Key())
		valide.SetAt = today
		if !connu {
			position[valide.Key()] = len(travaux)
			travaux = append(travaux, valide)
			bouge = true
			continue
		}
		// Redire la même date ne change rien : « set_at » ne bouge que quand
		// l'échéance bouge, sans quoi chaque enregistrement ferait un commit.
		if travaux[index].Due != valide.Due {
			travaux[index] = valide
			bouge = true
		}
	}
	if len(retires) == 0 {
		return travaux, bouge, nil
	}
	gardes := make([]Assignment, 0, len(travaux))
	for _, travail := range travaux {
		if retires[travail.Key()] {
			continue
		}
		gardes = append(gardes, travail)
	}
	return gardes, bouge, nil
}

// samePerson rend les positions des fiches de la même personne qu'une autre,
// elle comprise.
func samePerson(fiches []User, index int) []int {
	racines := rootsOf(fiches)
	memes := make([]int, 0, 2)
	for position, racine := range racines {
		if racine == racines[index] {
			memes = append(memes, position)
		}
	}
	return memes
}

// linked pose ou retire un renvoi, et dit si le registre a bougé.
//
// Redire une réunion qui existe déjà, ou défaire une qui n'existe plus, ne
// change rien plutôt que d'échouer : c'est ce qui rend le rejeu sûr quand un
// collègue a fait le même geste entre la lecture et l'écriture. C'est aux
// interfaces de dire avant d'écrire que le geste n'a pas lieu d'être.
func linked(fiches []User, lien Link, today string) ([]User, bool, error) {
	compte, err := valid.Login(lien.Username, "Compte GitHub")
	if err != nil {
		return nil, false, err
	}
	if strings.TrimSpace(lien.SameAs) == "" {
		separees := unlinked(fiches, compte)
		return separees, !sameLinks(fiches, separees), nil
	}
	principal, err := valid.Login(lien.SameAs, "Même personne que")
	if err != nil {
		return nil, false, err
	}
	if strings.EqualFold(compte, principal) {
		return nil, false, valid.Errorf(
			"@%s et @%s sont le même compte : il n'y a rien à réunir.", compte, principal)
	}

	// Un compte que le registre ignore y entre, nu : un enseignant ne figure
	// sur aucune liste, et c'est justement lui qui a le plus souvent deux
	// comptes. Son nom viendra d'ailleurs, ou de l'autre compte.
	ajoute := false
	for _, voulu := range []string{compte, principal} {
		if !slices.ContainsFunc(fiches, func(fiche User) bool {
			return strings.EqualFold(fiche.Username, voulu)
		}) {
			fiches = append(slices.Clone(fiches), User{Username: voulu, AddedAt: today})
			ajoute = true
		}
	}
	position := make(map[string]int, len(fiches))
	for index, fiche := range fiches {
		position[fiche.Key()] = index
	}
	racines := rootsOf(fiches)
	deCompte := racines[position[strings.ToLower(compte)]]
	duPrincipal := racines[position[strings.ToLower(principal)]]
	if deCompte == duPrincipal {
		return fiches, ajoute, nil
	}

	// Deux matricules différents sont deux étudiants pour le collège : c'est
	// la seule chose qui identifie vraiment quelqu'un, et le registre ne la
	// contredira pas sur la foi d'un clic.
	matricules := map[int]string{}
	for index, racine := range racines {
		if matricule := fiches[index].StudentID; matricule != "" && matricules[racine] == "" {
			matricules[racine] = matricule
		}
	}
	if a, b := matricules[deCompte], matricules[duPrincipal]; a != "" && b != "" &&
		!strings.EqualFold(a, b) {
		return nil, false, valid.Errorf(
			"@%s porte le matricule %s et @%s le matricule %s : pour le collège, ce "+
				"sont deux personnes. Si l'un des deux est erroné, corrigez-le d'abord.",
			compte, a, principal, b)
	}

	relies := slices.Clone(fiches)
	cible := relies[duPrincipal].Username
	for index, racine := range racines {
		if racine == deCompte {
			relies[index].SameAs = cible
		}
	}
	// La fiche qui désigne ne renvoie à personne, quoi que le fichier ait pu
	// lui faire dire à la main.
	relies[duPrincipal].SameAs = ""
	return relies, true, nil
}

// unlinked sépare un compte de sa personne. S'il la désignait, le suivant de
// ses comptes la désigne désormais : les autres restent ensemble.
func unlinked(fiches []User, compte string) []User {
	index := slices.IndexFunc(fiches, func(fiche User) bool {
		return strings.EqualFold(fiche.Username, compte)
	})
	if index < 0 {
		return fiches
	}
	racines := rootsOf(fiches)
	autres := make([]int, 0, 1)
	for position, racine := range racines {
		if position != index && racine == racines[index] {
			autres = append(autres, position)
		}
	}
	if len(autres) == 0 {
		return fiches
	}
	separees := slices.Clone(fiches)
	nouvelle := racines[index]
	if nouvelle == index {
		nouvelle = autres[0]
	}
	for _, autre := range autres {
		separees[autre].SameAs = ""
		if autre != nouvelle {
			separees[autre].SameAs = separees[nouvelle].Username
		}
	}
	separees[index].SameAs = ""
	return separees
}

// sameLinks dit si deux suites de fiches portent les mêmes renvois.
func sameLinks(gauche, droite []User) bool {
	return slices.EqualFunc(gauche, droite, func(a, b User) bool {
		return strings.EqualFold(a.SameAs, b.SameAs)
	})
}

// merge fond ce qu'on vient d'apprendre dans ce qu'on savait déjà.
//
// Le rôle n'en fait pas partie : il ne s'apprend pas, il se décide. « Roles »
// est le seul chemin qui le change. Le renvoi non plus, et pour la même
// raison : « Links » est le seul à l'écrire.
func merge(connu, appris User) User {
	// Un nom vide n'efface jamais un nom connu : apprendre un compte sans son
	// nom — ce que fait l'adoption de dépôts hérités — n'est pas l'oublier.
	if strings.TrimSpace(appris.FullName) != "" {
		connu.FullName = appris.FullName
	}
	// Le matricule non plus ne s'efface pas : c'est la seule chose qui
	// identifie vraiment quelqu'un, et un rapprochement qui l'ignore ne doit
	// pas faire oublier celui qu'une liste avait donné.
	if strings.TrimSpace(appris.StudentID) != "" {
		connu.StudentID = appris.StudentID
	}
	// Le compte garde son orthographe d'origine ; GitHub ne distingue pas la
	// casse, et en changer ferait un faux changement à chaque écriture.
	connu.Slugs = cleanSlugs(append(append([]string(nil), connu.Slugs...), appris.Slugs...))
	if connu.AddedAt == "" {
		connu.AddedAt = appris.AddedAt
	}
	return connu
}

// same compare deux fiches, pour n'écrire que ce qui a vraiment bougé : un
// commit qui ne change rien salit l'historique sans rien apprendre.
func same(left, right User) bool {
	return left.Username == right.Username && left.FullName == right.FullName &&
		left.StudentID == right.StudentID && left.AddedAt == right.AddedAt &&
		left.IsTeacher == right.IsTeacher && strings.EqualFold(left.SameAs, right.SameAs) &&
		strings.Join(left.Slugs, "\x00") == strings.Join(right.Slugs, "\x00")
}

// message compose ce que dira le commit. Il est écrit pour être lu : c'est
// l'historique du registre, et il tient lieu de traçabilité.
func (c Change) message() string {
	if raison := strings.TrimSpace(c.Reason); raison != "" {
		return raison
	}
	var parties []string
	if n := len(c.Learn); n > 0 {
		parties = append(parties, valid.Plural(n, "étudiant"))
	}
	if n := len(c.Forget); n > 0 {
		parties = append(parties, "retire "+valid.Plural(n, "compte"))
	}
	// Une échéance se lit dans l'historique comme le reste : c'est souvent la
	// seule trace de la date qu'un travail avait avant qu'on ne la déplace.
	if n := len(c.Deadlines); n > 0 {
		if len(parties) == 0 {
			return "Fixe la date de remise de " + valid.Plural(n, "travail", "travaux")
		}
		parties = append(parties, valid.Agree(n, "date", "dates")+" de remise de "+
			valid.Plural(n, "travail", "travaux"))
	}
	if len(parties) == 0 {
		return "Met le registre à jour"
	}
	return "Inscrit au registre : " + strings.Join(parties, ", ")
}

// -------------------------------------------------------------- lecture/écriture

// document est ce qui est écrit dans le dépôt.
//
// Les fiches se rangent sous « users » depuis la version 2, sous « students »
// avant elle. Les deux clés se relisent : une organisation amorcée par une
// version antérieure de l'outil ne doit pas perdre ses noms parce qu'un mot a
// changé. Seule « users » s'écrit.
type document struct {
	Version int    `json:"version"`
	Users   []User `json:"users"`
	// Legacy porte les fiches d'un fichier en version 1. Elle ne s'écrit
	// jamais : « omitempty » sur une tranche vide la tait.
	Legacy []User `json:"students,omitempty"`
}

// entries rend les fiches du document, d'où qu'elles viennent.
func (d document) entries() []User {
	if len(d.Users) > 0 {
		return d.Users
	}
	return d.Legacy
}

// Encode met le registre en forme. Le fichier est trié et indenté toujours
// pareil : c'est ce qui rend ses différences lisibles sur github.com, où deux
// personnes viendront les relire.
func (s *Set) Encode() ([]byte, error) {
	version := plainVersion
	if slices.ContainsFunc(s.users, func(fiche User) bool { return fiche.SameAs != "" }) {
		version = Version
	}
	payload, err := json.MarshalIndent(
		document{Version: version, Users: s.users}, "", "  ")
	if err != nil {
		return nil, valid.Errorf("Registre illisible à l'écriture : %v", err)
	}
	return append(payload, '\n'), nil
}

// Decode relit le registre. Une fiche que rien ne peut désigner est écartée et
// signalée, jamais fatale : quelqu'un modifiera ce fichier à la main sur
// github.com, et une virgule de trop ne doit pas priver toute l'organisation
// de ses noms.
func Decode(content []byte) (*Set, []string) {
	var lu document
	if err := json.Unmarshal(content, &lu); err != nil {
		return Empty(), []string{"Registre illisible : " + err.Error()}
	}
	var soucis []string
	// Une version qu'on ne connaît pas ne se devine pas : ce qu'elle range
	// ailleurs, ou nomme autrement, se lit comme absent. L'avertissement porte
	// donc d'abord sur ce qui est affiché — un registre qui paraît vide alors
	// qu'il est plein —, et seulement ensuite sur l'écriture. C'est arrivé : le
	// renommage de « students » en « users » a rendu, aux versions d'avant, une
	// organisation entière sans un seul nom, et sans un mot.
	if lu.Version > Version {
		soucis = append(soucis, fmt.Sprintf(
			"Le registre est en version %d, l'outil en connaît %d : ce qui s'affiche "+
				"peut être incomplet, ou vide alors que le fichier ne l'est pas. "+
				"Mettez l'outil à jour avant de vous y fier et avant d'y écrire.",
			lu.Version, Version))
	}
	lues := lu.entries()
	fiches := make([]User, 0, len(lues))
	position := map[string]int{}
	doublons := 0
	for _, fiche := range lues {
		valide, err := fiche.validate()
		if err != nil {
			soucis = append(soucis, "Fiche écartée : "+err.Error())
			continue
		}
		if strings.TrimSpace(fiche.SameAs) != "" && valide.SameAs == "" {
			soucis = append(soucis, "@"+valide.Username+" renvoie à « "+
				strings.TrimSpace(fiche.SameAs)+" », qui ne désigne aucun autre compte : "+
				"le renvoi est ignoré.")
		}
		if index, deja := position[valide.Key()]; deja {
			fusionnee, contredit := fondue(fiches[index], valide)
			fiches[index] = fusionnee
			if contredit {
				soucis = append(soucis, "Deux noms pour @"+fusionnee.Username+
					" : « "+fusionnee.FullName+" » est retenu.")
			} else {
				doublons++
			}
			continue
		}
		position[valide.Key()] = len(fiches)
		fiches = append(fiches, valide)
	}
	// Un renvoi vers un compte que le registre ignore se lit comme absent :
	// le taire laisserait croire réunis deux comptes qui ne le sont plus.
	for _, fiche := range fiches {
		if fiche.SameAs != "" && !slices.ContainsFunc(fiches, func(autre User) bool {
			return strings.EqualFold(autre.Username, fiche.SameAs)
		}) {
			soucis = append(soucis, "@"+fiche.Username+" renvoie à @"+fiche.SameAs+
				", que le registre ne connaît pas : le renvoi est ignoré.")
		}
	}
	if doublons > 0 {
		soucis = append(soucis, valid.Plural(doublons,
			"fiche en double réunie à la sienne",
			"fiches en double réunies aux leurs")+
			" : le fichier gagnerait à être nettoyé.")
	}
	return newSet(fiches, nil, rules.Rules{}, exchange.Catalog{}, signature.Book{},
		exchange.Asks{}), soucis
}

// fondue réunit deux fiches que le fichier donne pour un même compte, et dit si
// elles se contredisent.
//
// Relire n'est pas apprendre. Deux lignes pour un même compte sont une
// redondance du fichier, pas une intention : ce que l'une sait et l'autre pas
// est gardé. Les écarter perdait ce que la seconde disait — un nom que la
// première n'avait pas —, et le compte paraissait alors sans nom bien que le
// fichier le porte. Pire, sans nom, plus rien ne rattachait ses dépôts à lui :
// leur dernier niveau est le nom slugifié, et il n'y avait plus de nom à
// slugifier.
//
// Deux noms qui se contredisent, en revanche, ne se tranchent pas ici : le
// premier reste, et le désaccord se signale. Choisir au hasard serait pire que
// de le dire — c'est aussi ce que « sansMarque » fait de deux fiches que rien
// ne permet de confondre.
func fondue(gardee, autre User) (User, bool) {
	contredit := false
	if nom := strings.TrimSpace(autre.FullName); nom != "" {
		switch {
		case strings.TrimSpace(gardee.FullName) == "":
			gardee.FullName = nom
		case !strings.EqualFold(gardee.FullName, nom):
			contredit = true
		}
	}
	// Le matricule se comble sans jamais s'écraser : c'est la seule chose qui
	// identifie vraiment quelqu'un.
	if matricule := strings.TrimSpace(autre.StudentID); matricule != "" &&
		strings.TrimSpace(gardee.StudentID) == "" {
		gardee.StudentID = matricule
	}
	// Les slugs s'additionnent : chacun rattache des dépôts déjà créés, et en
	// perdre un les rendrait orphelins.
	gardee.Slugs = cleanSlugs(append(append([]string(nil), gardee.Slugs...), autre.Slugs...))
	// Le rôle ne s'oublie pas : un enseignant déclaré sur l'une des deux lignes
	// le reste.
	gardee.IsTeacher = gardee.IsTeacher || autre.IsTeacher
	// Un renvoi non plus : il a été décidé, et la ligne qui le porte le dit.
	if gardee.SameAs == "" {
		gardee.SameAs = autre.SameAs
	}
	// La plus ancienne date d'ajout l'emporte : c'est celle qui dit depuis
	// quand ce compte est connu.
	if autre.AddedAt != "" && (gardee.AddedAt == "" || autre.AddedAt < gardee.AddedAt) {
		gardee.AddedAt = autre.AddedAt
	}
	return gardee, contredit
}

// Readme explique le dépôt à qui l'ouvre sur github.com sans savoir ce que
// c'est. Il est écrit une fois, au premier commit.
func Readme(org string) []byte {
	return []byte(`# Registre de ` + org + `

Ce dépôt appartient à l'extension ` + "`" + brand.Command + "`" + `. Il retient ce que les noms
de dépôts ne peuvent pas dire, pour tout le monde et depuis n'importe quel poste.

## ` + "`" + UsersFile + "`" + ` — qui est derrière chaque dépôt

Les dépôts d'étudiants sont nommés ` + "`session.cours.groupe.travail.étudiant`" + `,
et le dernier niveau est le nom slugifié — pas le compte. Rien dans les noms de
dépôts ne dit donc à qui ils appartiennent : c'est ce fichier qui le dit, un
**nom complet en face de chaque compte GitHub**. Il porte aussi **le rôle** que
chacun tient dans l'organisation — étudiant ou enseignant.

## ` + "`" + AssignmentsFile + "`" + ` — quand chaque travail est attendu

Une **date de remise** par travail, sous son identifiant complet
(` + "`a26.5n6.01.tp1`" + `). Aucun nom de dépôt ne peut porter une échéance, et la
retenir sur un poste la rendrait vraie d'une seule machine : votre collègue
verrait les mêmes travaux sans voir la date.

## Le rôle ne donne aucun droit

` + "`is_teacher`" + ` nomme, il n'autorise pas. Personne n'accède à quoi que ce
soit parce que ce champ vaut ` + "`true`" + ` : ce sont les droits GitHub —
appartenance à l'organisation, équipes, collaborateurs — qui décident de ce que
chacun peut lire. C'est l'inverse qui est vrai, et c'est ce qui protège le
champ : seul quelqu'un qui a déjà le droit d'écrire ici peut le mettre à
` + "`true`" + `, et un étudiant ne l'a jamais eu.

## Ce qu'il ne contient pas

Ni les inscriptions aux groupes, ni les réglages de distribution : ceux-là
restent sur le poste de la personne qui enseigne.

## Précautions

Ce dépôt porte des renseignements personnels. Il doit rester **privé** —
l'outil refuse d'y écrire s'il devient public — et son accès mérite d'être
restreint à l'équipe enseignante.

Il se modifie à la main sans risque : une fiche mal écrite est écartée et
signalée, elle ne rend pas le reste illisible.
`)
}
