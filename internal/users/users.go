// Package users dresse la liste des utilisateurs d'une organisation — chacun
// avec les dépôts qu'il a déjà —, puis la filtre et la trie.
//
// « Utilisateur » est le mot qui englobe : un étudiant est un utilisateur, un
// enseignant aussi. La liste d'un groupe ne montre que ses étudiants ;
// l'annuaire de l'organisation montre tout le monde, et c'est là que la
// distinction se voit. Le rôle vient du registre, jamais du nom d'un dépôt :
// rien dans « a26.5n6.01.tp1.emilie-cote » ne dit qui enseigne.
//
// Le filtre et le tri vivent ici plutôt que dans une interface : « trié par
// dernier envoi » doit donner le même ordre au navigateur et au terminal, et
// « avant le 1er octobre » doit y vouloir dire la même chose. Une interface ne
// fait que dire ce qu'elle veut ; c'est ce paquet qui sait ce que cela signifie.
package users

import (
	"sort"
	"strings"
	"time"

	"github.com/PierreOlivierBrillant/gh-milou/internal/classroom"
	"github.com/PierreOlivierBrillant/gh-milou/internal/groups"
	"github.com/PierreOlivierBrillant/gh-milou/internal/roster"
	"github.com/PierreOlivierBrillant/gh-milou/internal/teams"
	"github.com/PierreOlivierBrillant/gh-milou/internal/valid"
)

// DateFormat est la forme attendue d'une date de filtre.
const DateFormat = "2006-01-02"

// Repo est un dépôt qu'un étudiant possède déjà.
type Repo struct {
	// Assignment est le nom court du travail, ID son identifiant complet.
	Assignment string
	ID         string
	Name       string
	URL        string
	// PushedAt est le dernier envoi, à l'heure de la machine — « 2026-08-21
	// 14:32 » —, vide si rien n'y a été envoyé.
	PushedAt string
	// Team nomme l'équipe à qui le dépôt appartient ; vide pour un travail
	// individuel. Un dépôt d'équipe figure chez chacun de ses membres : c'est
	// bien leur travail, même s'il ne porte pas leur nom.
	Team string
	// Scope est la place du groupe d'où vient ce dépôt. Elle ne sert que
	// lorsqu'une ligne en rassemble plusieurs : deux groupes peuvent avoir
	// chacun leur « tp1 », et rien d'autre ne les distinguerait.
	Scope string
}

// Row est un étudiant et ce que ses dépôts racontent de lui.
type Row struct {
	FullName string
	Username string
	// Accounts porte tous les comptes de la personne : elle n'en a qu'un le
	// plus souvent, et parfois deux — celui du collège et le sien.
	Accounts []string
	// StudentID est le matricule, quand une liste du collège l'a donné. C'est
	// lui qui a réuni les comptes de cette ligne.
	StudentID string
	// IsTeacher est ce que le registre déclare de cette personne. Il ne se lit
	// nulle part ailleurs : rien dans un nom de dépôt ne dit qui enseigne.
	IsTeacher bool
	Repos     []Repo
	// Enrollments dit les groupes dont la personne est. La liste d'un groupe
	// n'en porte qu'un ; l'annuaire de l'organisation les rassemble tous.
	Enrollments []Enrollment
	// PushedAt est le plus récent envoi de ses dépôts ; vide s'il n'y en a eu aucun.
	PushedAt string
}

// Role nomme ce que la personne est, du mot que les trois interfaces écrivent.
func (r Row) Role() string {
	if r.IsTeacher {
		return AsTeacher
	}
	return AsStudent
}

// Build croise les étudiants du groupe avec les dépôts de l'organisation :
// c'est l'équivalent du « a accepté le devoir » de GitHub Classroom, déduit des
// dépôts existants plutôt que d'une invitation.
//
// Les équipes du groupe entrent dans le compte : le dépôt d'une équipe est
// celui de chacun de ses membres. Sans elles, un travail fait en équipe
// laisserait tout le monde à « aucun dépôt », alors que tout le monde en a un.
func Build(cours classroom.Classroom, repos []groups.RepoInfo,
	equipes []teams.Team) []Row {
	// Un dépôt est rangé sous ce qui désigne sa personne : son compte, à défaut
	// son matricule. Rangés sous un compte vide, ceux de qui n'en a pas encore
	// n'étaient à personne, et la liste lui disait « aucun dépôt ».
	parEtudiant := map[string][]Repo{}
	scope := cours.Scope()
	ajouter := func(personne roster.Person, depot Repo) {
		cle := classroom.Designation(personne)
		parEtudiant[cle] = append(parEtudiant[cle], depot)
	}

	for _, travail := range cours.Assignments(repos, equipes) {
		for _, depot := range cours.Repos(travail.ID, repos) {
			ligne := Repo{
				Assignment: travail.Name, ID: travail.ID, Name: depot.Name,
				URL: depot.URL, PushedAt: depot.PushedAt, Scope: scope,
			}
			if equipe, appartient := cours.TeamOf(depot.Name, equipes); appartient {
				ligne.Team = equipe.Short
				for _, membre := range equipe.Members {
					ajouter(roster.Person{Username: membre}, ligne)
				}
				continue
			}
			student, inscrit := cours.StudentOf(depot.Name)
			if !inscrit {
				continue
			}
			ajouter(student, ligne)
		}
	}

	// Une ligne par personne, non par compte : c'est le matricule qui les
	// réunit, et les montrer deux fois ferait deux étudiants d'un seul.
	identites := cours.Identities()
	lignes := make([]Row, 0, len(identites))
	for _, identite := range identites {
		// Les dépôts d'une personne peuvent être arrivés sous l'un ou l'autre
		// de ses comptes, ou sous son seul matricule : ils sont les siens sous
		// chacun.
		depots := make([]Repo, 0)
		vus := map[string]bool{}
		for _, cle := range identite.Designations() {
			for _, depot := range parEtudiant[cle] {
				if vus[strings.ToLower(depot.Name)] {
					continue
				}
				vus[strings.ToLower(depot.Name)] = true
				depots = append(depots, depot)
			}
		}
		ligne := compose(identite.FullName, identite.Username(), depots)
		ligne.Accounts, ligne.StudentID = identite.Accounts, identite.StudentID
		ligne.Enrollments = []Enrollment{enrollmentOf(cours, ligne.Repos)}
		lignes = append(lignes, ligne)
	}
	return lignes
}

// FromGroup dresse les mêmes lignes à partir d'un groupe lu par préfixe : c'est
// ainsi que l'assistant du terminal travaille, un dépôt par personne. Les noms
// complets, qu'un préfixe ne dit pas, sont fournis à part — dépôt par dépôt.
func FromGroup(group groups.Group, names map[string]string) []Row {
	lignes := make([]Row, 0, group.Len())
	for _, depot := range group.Repos {
		lignes = append(lignes, compose(names[depot.Name], depot.Suffix, []Repo{{
			Assignment: group.Prefix, ID: group.Prefix, Name: depot.Name,
			URL: depot.URL, PushedAt: depot.PushedAt,
		}}))
	}
	return lignes
}

// compose assemble une ligne et en retient le plus récent envoi.
func compose(fullName, username string, depots []Repo) Row {
	if depots == nil {
		depots = []Repo{}
	}
	ligne := Row{
		FullName: fullName, Username: username, Repos: depots,
		Enrollments: []Enrollment{},
	}
	for _, depot := range depots {
		if depot.PushedAt > ligne.PushedAt {
			ligne.PushedAt = depot.PushedAt
		}
	}
	return ligne
}

// ------------------------------------------------------------------- filtres

// Activity retient les étudiants selon ce que leurs dépôts montrent d'eux.
type Activity string

const (
	// AnyActivity ne retient rien : tout le monde passe.
	AnyActivity Activity = ""
	// WithRepos ne garde que ceux qui ont au moins un dépôt.
	WithRepos Activity = "avec"
	// WithoutRepos ne garde que ceux qui n'en ont aucun.
	WithoutRepos Activity = "sans"
	// Silent ne garde que ceux qui ont un dépôt mais n'y ont jamais rien envoyé.
	Silent Activity = "muet"
)

// Activities énumère les valeurs acceptées, dans l'ordre où les proposer.
var Activities = []Activity{AnyActivity, WithRepos, WithoutRepos, Silent}

// Filter retient les étudiants qui répondent à des critères. Un critère laissé
// vide ne retient rien : il laisse simplement passer.
//
// Les dates portent sur le dernier envoi de la personne, bornes incluses.
// Quelqu'un qui n'a jamais rien envoyé n'a pas de date : il est écarté des deux
// bornes plutôt que rangé arbitrairement d'un côté. « sans » et « muet » sont
// là pour le retrouver.
type Filter struct {
	// Text est cherché à la fois dans le nom complet et dans le compte.
	Text string
	// Name et Username cherchent dans l'un ou dans l'autre seulement.
	Name     string
	Username string
	// Assignment ne garde que ceux qui ont un dépôt pour ce travail, désigné
	// par son nom court ou par son identifiant complet.
	Assignment string
	// Role ne garde que les enseignants, ou que les étudiants. C'est le
	// registre qui l'a dit : rien dans un nom de dépôt ne le dirait.
	Role Role
	// Session et Course ne gardent que ceux qui ont suivi cette session, ou ce
	// cours. Ils portent sur l'inscription, non sur les dépôts : quelqu'un
	// d'inscrit qui n'a rien remis a suivi le cours quand même.
	Session string
	Course  string
	// PushedAfter et PushedBefore encadrent la journée du dernier envoi,
	// « 2026-10-01 ».
	PushedAfter  string
	PushedBefore string
	Activity     Activity
}

// Role désigne ce qu'une personne est dans l'organisation.
type Role string

const (
	// AnyRole ne retient rien : étudiants et enseignants passent.
	AnyRole Role = ""
	// OnlyTeachers ne garde que ceux qui enseignent, OnlyStudents que les autres.
	OnlyTeachers Role = "enseignant"
	OnlyStudents Role = "etudiant"
)

// Roles énumère les valeurs acceptées, dans l'ordre où les proposer.
var Roles = []Role{AnyRole, OnlyTeachers, OnlyStudents}

// ParseRole valide un rôle saisi. Il se lit accentué comme non accentué :
// « étudiant » se tape rarement avec son accent au terminal.
func ParseRole(value string) (Role, error) {
	role := Role(valid.Slugify(value))
	for _, candidate := range Roles {
		if role == Role(valid.Slugify(string(candidate))) {
			return candidate, nil
		}
	}
	return AnyRole, valid.Errorf(
		"Rôle : « %s » est inconnu (attendu : enseignant, étudiant, ou rien).", value)
}

// Validate met le filtre en forme et refuse ce qui ne peut pas être appliqué.
func (f Filter) Validate() (Filter, error) {
	after, err := ParseDate(f.PushedAfter, "Dernier envoi après")
	if err != nil {
		return f, err
	}
	before, err := ParseDate(f.PushedBefore, "Dernier envoi avant")
	if err != nil {
		return f, err
	}
	if after != "" && before != "" && after > before {
		return f, valid.Errorf(
			"Dernier envoi : « %s » est postérieur à « %s ».", after, before)
	}
	f.PushedAfter, f.PushedBefore = after, before

	activite := Activity(strings.ToLower(strings.TrimSpace(string(f.Activity))))
	connue := false
	for _, candidate := range Activities {
		if candidate == activite {
			connue = true
			break
		}
	}
	if !connue {
		return f, valid.Errorf(
			"Activité : « %s » est inconnu (attendu : avec, sans, muet, ou rien).", f.Activity)
	}
	f.Activity = activite
	role, err := ParseRole(string(f.Role))
	if err != nil {
		return f, err
	}
	f.Role = role
	f.Text = strings.TrimSpace(f.Text)
	f.Name = strings.TrimSpace(f.Name)
	f.Username = strings.TrimSpace(f.Username)
	f.Assignment = strings.TrimSpace(f.Assignment)
	f.Session = strings.TrimSpace(f.Session)
	f.Course = strings.TrimSpace(f.Course)
	return f, nil
}

// ParseDate valide une date de filtre et la renvoie sous sa forme normale.
func ParseDate(value, label string) (string, error) {
	date := strings.TrimSpace(value)
	if date == "" {
		return "", nil
	}
	parsed, err := time.Parse(DateFormat, date)
	if err != nil {
		return "", valid.Errorf("%s : « %s » n'est pas une date (attendu AAAA-MM-JJ).",
			label, date)
	}
	return parsed.Format(DateFormat), nil
}

// Keep dit si une ligne passe le filtre.
func (f Filter) Keep(row Row) bool {
	if !contains(row.FullName+" "+row.Username, f.Text) ||
		!contains(row.FullName, f.Name) ||
		!contains(row.Username, f.Username) {
		return false
	}
	if !f.keepRole(row) || !f.keepActivity(row) ||
		!f.keepAssignment(row) || !f.keepEnrollment(row) {
		return false
	}
	// Sans date connue, les bornes ne peuvent rien dire de cette personne. Les
	// bornes portent sur la journée : « avant le 1er octobre » englobe le 1er
	// octobre entier, quelle que soit l'heure du dernier envoi.
	jour := valid.Day(row.PushedAt)
	if f.PushedAfter != "" && (jour == "" || jour < f.PushedAfter) {
		return false
	}
	if f.PushedBefore != "" && (jour == "" || jour > f.PushedBefore) {
		return false
	}
	return true
}

// keepRole retient une personne selon ce qu'elle est.
func (f Filter) keepRole(row Row) bool {
	switch f.Role {
	case OnlyTeachers:
		return row.IsTeacher
	case OnlyStudents:
		return !row.IsTeacher
	}
	return true
}

func (f Filter) keepActivity(row Row) bool {
	switch f.Activity {
	case WithRepos:
		return len(row.Repos) > 0
	case WithoutRepos:
		return len(row.Repos) == 0
	case Silent:
		return len(row.Repos) > 0 && row.PushedAt == ""
	}
	return true
}

func (f Filter) keepAssignment(row Row) bool {
	if f.Assignment == "" {
		return true
	}
	for _, depot := range row.Repos {
		if strings.EqualFold(depot.Assignment, f.Assignment) ||
			strings.EqualFold(depot.ID, f.Assignment) {
			return true
		}
	}
	return false
}

// keepEnrollment retient ceux dont un des groupes répond aux deux critères à
// la fois : « le cours 5N6 à l'automne 2026 » ne doit pas retenir quelqu'un qui
// a fait ce cours une autre session et un autre cours cet automne-là.
func (f Filter) keepEnrollment(row Row) bool {
	if f.Session == "" && f.Course == "" {
		return true
	}
	for _, inscription := range row.Enrollments {
		if f.Session != "" && !strings.EqualFold(inscription.Session, f.Session) {
			continue
		}
		if f.Course != "" && !strings.EqualFold(inscription.Course, f.Course) {
			continue
		}
		return true
	}
	return false
}

// contains cherche une sous-chaîne sans tenir compte de la casse ni des
// accents : « cote » doit trouver « Côté », que la personne se soit inscrite
// avec ou sans clavier accentué.
func contains(haystack, needle string) bool {
	wanted := valid.Slugify(needle)
	if wanted == "" {
		return true
	}
	return strings.Contains(valid.Slugify(haystack), wanted)
}

// --------------------------------------------------------------------- tri

// Key désigne la colonne sur laquelle trier.
type Key string

const (
	// ByName trie par nom complet.
	ByName Key = "nom"
	// ByUsername trie par compte GitHub.
	ByUsername Key = "compte"
	// ByPushed trie par date du dernier envoi.
	ByPushed Key = "envoi"
)

// Keys énumère les tris acceptés, dans l'ordre où les proposer.
var Keys = []Key{ByName, ByUsername, ByPushed}

// ParseKey valide un tri saisi. Sans valeur, c'est le nom.
func ParseKey(value string) (Key, error) {
	key := Key(strings.ToLower(strings.TrimSpace(value)))
	if key == "" {
		return ByName, nil
	}
	for _, candidate := range Keys {
		if candidate == key {
			return key, nil
		}
	}
	return ByName, valid.Errorf(
		"Tri : « %s » est inconnu (attendu : nom, compte ou envoi).", value)
}

// Apply filtre puis trie une liste d'étudiants, sans toucher à l'originale.
//
// Le tri est total : à valeur égale, le nom puis le compte tranchent, si bien
// que deux appels donnent toujours le même ordre. Ce qui manque — une date,
// un nom complet — se range avant tout le reste : en tête d'un tri croissant,
// en queue d'un tri décroissant.
func Apply(rows []Row, filter Filter, key Key, desc bool) []Row {
	retenues := make([]Row, 0, len(rows))
	for _, row := range rows {
		if filter.Keep(row) {
			retenues = append(retenues, row)
		}
	}
	sort.SliceStable(retenues, func(i, j int) bool {
		gauche, droite := retenues[i], retenues[j]
		if compare := compareOn(key, gauche, droite); compare != 0 {
			return (compare < 0) != desc
		}
		return fallback(gauche, droite)
	})
	return retenues
}

// compareOn compare deux lignes sur la colonne demandée.
func compareOn(key Key, gauche, droite Row) int {
	switch key {
	case ByUsername:
		return strings.Compare(fold(gauche.Username), fold(droite.Username))
	case ByPushed:
		return strings.Compare(gauche.PushedAt, droite.PushedAt)
	}
	return strings.Compare(fold(gauche.FullName), fold(droite.FullName))
}

// fallback départage deux lignes que la colonne de tri laisse à égalité.
func fallback(gauche, droite Row) bool {
	if gauche.FullName != droite.FullName {
		return fold(gauche.FullName) < fold(droite.FullName)
	}
	return fold(gauche.Username) < fold(droite.Username)
}

// fold rend un texte comparable : « Émilie » se range avec les E.
func fold(value string) string { return valid.Slugify(value) }
