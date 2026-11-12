// Package classroom tient la notion de groupe : des étudiants, une place dans
// la nomenclature des dépôts, et les réglages que ses travaux reprennent.
//
// GitHub reste la source de vérité. Un travail n'est pas une fiche enregistrée
// quelque part : c'est l'ensemble des dépôts nommés
// « session.cours.groupe.travail.étudiant », lus dans l'organisation. Le groupe
// ne retient que ce que les noms de dépôts ne savent pas dire — qui sont les
// étudiants, et avec quels réglages leurs dépôts sont créés. Un groupe se
// déclare donc sans rien écrire sur GitHub, et se supprime sans rien y effacer.
//
// Un travail est individuel ou d'équipe, et cela non plus ne se déclare pas :
// le dernier niveau du nom de ses dépôts nomme une équipe du groupe, ou un
// étudiant. Les équipes elles-mêmes vivent sur GitHub — ce sont de vraies
// équipes d'organisation —, si bien qu'elles sont passées en argument là où
// elles comptent plutôt que retenues ici.
package classroom

import (
	"slices"
	"sort"
	"strings"

	"github.com/PierreOlivierBrillant/gh-milou/internal/config"
	"github.com/PierreOlivierBrillant/gh-milou/internal/groups"
	"github.com/PierreOlivierBrillant/gh-milou/internal/naming"
	"github.com/PierreOlivierBrillant/gh-milou/internal/plan"
	"github.com/PierreOlivierBrillant/gh-milou/internal/roster"
	"github.com/PierreOlivierBrillant/gh-milou/internal/teams"
	"github.com/PierreOlivierBrillant/gh-milou/internal/valid"
)

// NamePattern est le gabarit des noms de dépôts. Il n'est pas réglable : c'est
// la nomenclature elle-même, et tout le reste en dépend.
const NamePattern = "{assignment}" + naming.Separator + "{name}"

// PlanSettings rend les réglages d'un travail tels que le plan doit les suivre.
//
// Un travail de la nomenclature — « a26.5n6.01.tp1 » — nomme ses dépôts par
// elle, quel que soit le gabarit retenu pour les préfixes hérités : c'est ainsi
// que le navigateur les nomme, et que son groupe les reconnaît. Le gabarit
// retenu n'est pas touché pour autant : il reste celui du prochain préfixe
// hérité, que ce travail-ci n'a pas à changer.
func PlanSettings(settings config.Settings) config.Settings {
	if _, _, situe := naming.SplitAssignment(settings.Assignment); situe {
		settings.NamePattern = NamePattern
	}
	return settings
}

// Defaults rassemble les réglages que les travaux du groupe reprennent.
type Defaults struct {
	DescriptionPattern string  `json:"description_pattern"`
	Template           string  `json:"template"`
	Visibility         string  `json:"visibility"`
	Permission         string  `json:"permission"`
	AddCollaborator    bool    `json:"add_collaborator"`
	StarterDir         string  `json:"starter_dir"`
	CommitMessage      string  `json:"commit_message"`
	DelaySeconds       float64 `json:"delay_seconds"`
}

// DefaultsFrom reprend les réglages généraux comme point de départ d'un groupe.
func DefaultsFrom(settings config.Settings) Defaults {
	return Defaults{
		DescriptionPattern: settings.DescriptionPattern,
		Template:           settings.Template,
		Visibility:         settings.Visibility,
		Permission:         settings.Permission,
		AddCollaborator:    settings.AddCollaborator,
		StarterDir:         settings.StarterDir,
		CommitMessage:      settings.CommitMessage,
		DelaySeconds:       settings.DelaySeconds,
	}
}

// normalized comble les valeurs absentes par celles de l'outil.
func (d Defaults) normalized() Defaults {
	repli := config.Default()
	if strings.TrimSpace(d.DescriptionPattern) == "" {
		d.DescriptionPattern = repli.DescriptionPattern
	}
	if strings.TrimSpace(d.Visibility) == "" {
		d.Visibility = repli.Visibility
	}
	if strings.TrimSpace(d.Permission) == "" {
		d.Permission = repli.Permission
	}
	if strings.TrimSpace(d.CommitMessage) == "" {
		d.CommitMessage = repli.CommitMessage
	}
	if d.DelaySeconds < 0 {
		d.DelaySeconds = repli.DelaySeconds
	}
	return d
}

// Classroom est un groupe : une session, un cours, une organisation, des
// étudiants. Seul le nom court de la session entre dans les dépôts ; son nom
// long vit dans le magasin, partagé par tous les groupes de la session.
type Classroom struct {
	Org        string          `json:"org"`
	Session    string          `json:"session"`
	Course     string          `json:"course"`
	Group      string          `json:"group"`
	Students   []roster.Person `json:"students"`
	RosterPath string          `json:"roster_path,omitempty"`
	Defaults   Defaults        `json:"defaults"`
	CreatedAt  string          `json:"created_at"`

	// derived retient les comptes que le registre a révélés plutôt que la
	// liste locale. Il n'est pas écrit : ce qui a été déduit ne doit pas se
	// faire passer pour ce qui a été déclaré.
	derived map[string]bool
	// aliases retient les fragments que le registre sait rattacher à quelqu'un
	// sans que le nom courant les produise — un nom corrigé depuis, un dépôt
	// adopté sous un autre nom. Sans eux, corriger une faute de frappe
	// détacherait de leur personne tous les dépôts déjà créés.
	aliases map[string]roster.Person
	// borrowed retient, par compte, les autres comptes que le registre a
	// joints à une personne de la liste. Ils ne sont pas écrits non plus : la
	// réunion vit au registre, et la recopier ici la rendrait indéfaisable —
	// séparer les deux comptes là-bas les laisserait réunis dans ce groupe.
	borrowed map[string][]string
	// horaire dit quand les travaux du groupe sont attendus. Il n'est pas
	// écrit non plus : les dates vivent dans le registre de l'organisation, et
	// les redire ici en ferait un second exemplaire libre de diverger.
	horaire Schedule
	// enseignants dit qui, dans l'organisation, enseigne. Comme l'horaire, il
	// est branché plutôt que retenu : c'est ce qui permet de ne pas prendre
	// pour une remise ce qu'un enseignant a poussé dans le dépôt d'un étudiant.
	enseignants Teachers
}

// Validate met le groupe en forme et refuse ce qui ne peut pas nommer un dépôt.
func (c Classroom) Validate() (Classroom, error) {
	org, err := valid.Login(c.Org, "Organisation")
	if err != nil {
		return c, err
	}
	c.Org = org

	session, err := naming.Fragment(c.Session, "Session")
	if err != nil {
		return c, err
	}
	course, err := naming.Fragment(c.Course, "Cours")
	if err != nil {
		return c, err
	}
	group, err := naming.Fragment(c.Group, "Groupe")
	if err != nil {
		return c, err
	}
	c.Session, c.Course, c.Group = session, course, group

	if strings.TrimSpace(c.Defaults.DescriptionPattern) != "" {
		if _, err := plan.ValidatePattern(
			c.Defaults.DescriptionPattern, "Gabarit de description", false); err != nil {
			return c, err
		}
	}
	c.Defaults = c.Defaults.normalized()
	c.Students = dedupe(c.Students)
	return c, nil
}

// Label nomme le groupe. Il n'y a rien à retenir pour cela : la place dit tout,
// et elle est dans le nom de chacun de ses dépôts.
func (c Classroom) Label() string { return "Groupe " + c.Group }

// SessionName rend le nom long de la session du groupe.
func (c Classroom) SessionName() string { return naming.SessionLabel(c.Session) }

// Session identifie une session : un nom court pour les dépôts, un nom long
// pour l'affichage.
type Session struct {
	Short string `json:"short"`
	Name  string `json:"name"`
}

// SessionsOf rassemble des noms courts en sessions nommées, sans doublon et de
// la plus récente à la plus ancienne. Toutes les interfaces passent par ici :
// l'ordre des sessions ne se décide qu'une fois.
func SessionsOf(shorts []string) []Session {
	vues := map[string]bool{}
	sessions := make([]Session, 0, len(shorts))
	for _, court := range shorts {
		court = strings.TrimSpace(court)
		if court == "" || vues[strings.ToLower(court)] {
			continue
		}
		vues[strings.ToLower(court)] = true
		sessions = append(sessions, Session{Short: court, Name: SessionName(court)})
	}
	sort.Slice(sessions, func(i, j int) bool {
		return naming.CompareSessions(sessions[i].Short, sessions[j].Short) < 0
	})
	return sessions
}

// LatestSession ne garde, parmi des groupes, que ceux de la session la plus
// récente : tous les cours de cette session-là, et rien des précédentes. La
// règle est celle de « SessionsOf » — ce qui tombe sous les yeux en premier est
// aussi ce qu'on prépare d'avance.
//
// Les sessions passées ne bougent plus : leurs dépôts ne recevront plus rien,
// et leurs accès ne changeront plus.
func LatestSession(items []Classroom) []Classroom {
	courts := make([]string, 0, len(items))
	for _, item := range items {
		courts = append(courts, item.Session)
	}
	sessions := SessionsOf(courts)
	if len(sessions) == 0 {
		return nil
	}
	gardes := make([]Classroom, 0, len(items))
	for _, item := range items {
		if strings.EqualFold(strings.TrimSpace(item.Session), sessions[0].Short) {
			gardes = append(gardes, item)
		}
	}
	return gardes
}

// Scope désigne ce que le groupe couvre : son préfixe dans les noms de dépôts.
func (c Classroom) Scope() string {
	return naming.Prefix(c.Session, c.Course, c.Group)
}

// AssignmentID compose l'identifiant complet d'un travail du groupe : c'est lui
// qui précède le nom de l'étudiant dans le nom du dépôt.
func (c Classroom) AssignmentID(name string) string {
	return naming.AssignmentID(c.Session, c.Course, c.Group, strings.TrimSpace(name))
}

// ShortName retire du travail ce qui désigne le groupe.
func (c Classroom) ShortName(id string) string {
	scope := c.Scope()
	if scope != "" && len(id) > len(scope)+1 &&
		strings.EqualFold(id[:len(scope)+1], scope+naming.Separator) {
		return id[len(scope)+1:]
	}
	return id
}

// Owns dit si un identifiant de travail relève du groupe.
func (c Classroom) Owns(id string) bool {
	if strings.TrimSpace(id) == "" {
		return false
	}
	scope := c.Scope()
	if scope == "" {
		return false
	}
	return len(id) > len(scope)+1 &&
		strings.EqualFold(id[:len(scope)+1], scope+naming.Separator)
}

// Settings compose les réglages d'un travail du groupe, prêts pour le plan.
func (c Classroom) Settings(assignmentName string) config.Settings {
	defauts := c.Defaults.normalized()
	settings := config.Default()
	settings.Org = c.Org
	settings.Assignment = c.AssignmentID(assignmentName)
	settings.NamePattern = NamePattern
	settings.DescriptionPattern = defauts.DescriptionPattern
	settings.Template = defauts.Template
	settings.Visibility = defauts.Visibility
	settings.Permission = defauts.Permission
	settings.AddCollaborator = defauts.AddCollaborator
	settings.StarterDir = defauts.StarterDir
	settings.CommitMessage = defauts.CommitMessage
	settings.DelaySeconds = defauts.DelaySeconds
	settings.RosterPath = c.RosterPath
	return settings
}

// MissingNames renvoie les étudiants dont le nom complet manque : leur dépôt ne
// peut pas être nommé.
func (c Classroom) MissingNames() []roster.Person {
	var incomplets []roster.Person
	for _, identite := range c.Identities() {
		if _, err := naming.Student(identite.FullName); err != nil {
			incomplets = append(incomplets, identite.Person())
		}
	}
	return incomplets
}

// Names répond à la seule question que le registre de l'organisation permet de
// poser : qui se cache derrière le dernier niveau d'un nom de dépôt ?
//
// Le paquet ne connaît du registre que cette question. Il pourrait sinon en
// dépendre entièrement — et donc du client GitHub et du cache —, alors qu'il
// n'a besoin de rien d'autre.
type Names interface {
	Lookup(fragment string) (roster.Person, bool)
}

// Enrich verse dans le groupe ce que le registre de l'organisation sait de ses
// personnes, et rend le groupe ainsi complété.
//
// Deux manques s'y comblent. Les noms d'abord : une liste peut ne porter qu'un
// compte — c'est le cas de tout groupe adopté depuis des dépôts hérités —, et
// le registre dit qui il désigne. Les inscriptions ensuite : un groupe qu'on
// n'a pas déclaré sur cette machine, celui d'un collègue, n'a aucune liste ;
// ses étudiants se lisent alors dans ses dépôts, chaque dernier niveau menant
// au registre.
//
// Ce qui est ainsi déduit n'est pas écrit sur le disque : ce que la machine
// déclare doit rester ce qu'on lui a dit, non ce qu'elle a conclu. Le magasin
// s'en charge à l'enregistrement.
func (c Classroom) Enrich(names Names, repos []groups.RepoInfo) Classroom {
	if names == nil {
		return c
	}
	complets := make([]roster.Person, 0, len(c.Students))
	connus := map[string]bool{}
	empruntes := map[string][]string{}
	for _, student := range c.Students {
		if trouve, ok := names.Lookup(student.Username); ok {
			if strings.TrimSpace(student.FullName) == "" {
				student.FullName = trouve.FullName
			}
			// Le matricule vient du registre comme le nom : c'est lui qui
			// réunit les comptes d'une personne, et il doit valoir d'un poste
			// à l'autre.
			if strings.TrimSpace(student.StudentID) == "" {
				student.StudentID = trouve.StudentID
			}
			// Ses autres comptes aussi : réunis une fois au registre, ils
			// valent pour tous ses groupes. Elle est alors invitée sous
			// chacun, et un dépôt arrivé sous l'un est le sien.
			for _, compte := range trouve.Accounts() {
				if student.Owns(compte) {
					continue
				}
				student.Also = append(slices.Clone(student.Also), compte)
				cle := strings.ToLower(student.Username)
				empruntes[cle] = append(empruntes[cle], compte)
			}
		}
		// Tous ses comptes comptent comme connus : un dépôt arrivé sous l'un
		// d'eux ne doit pas la faire inscrire une seconde fois.
		for _, compte := range student.Accounts() {
			connus[strings.ToLower(compte)] = true
		}
		complets = append(complets, student)
	}

	deduits := map[string]bool{}
	alias := map[string]roster.Person{}
	for _, repo := range repos {
		fragment, lisible := c.Fragment(repo.Name)
		if !lisible || !c.Owns(repo.Name) {
			continue
		}
		trouve, ok := names.Lookup(fragment)
		if !ok {
			continue
		}
		// Le fragment est retenu même quand la personne est déjà de la liste :
		// c'est lui qui rattache un dépôt que le nom courant ne produit plus.
		alias[strings.ToLower(fragment)] = trouve
		if connus[strings.ToLower(trouve.Username)] {
			continue
		}
		connus[strings.ToLower(trouve.Username)] = true
		deduits[strings.ToLower(trouve.Username)] = true
		complets = append(complets, trouve)
	}

	c.Students = complets
	c.derived, c.aliases, c.borrowed = deduits, alias, empruntes
	return c
}

// Trimmed retire de la liste les noms que le registre porte déjà à l'identique.
// Le fichier local n'a plus à les redire : le registre en est la source, et
// deux exemplaires d'un même nom finissent toujours par diverger.
//
// Un nom que le registre ignore, ou qu'il porte autrement, reste écrit ici.
// Rien ne doit se perdre parce que la publication n'a pas encore eu lieu, ni
// qu'un désaccord n'a pas été tranché.
func (c Classroom) Trimmed(names Names) Classroom {
	if names == nil {
		return c
	}
	allegee := make([]roster.Person, 0, len(c.Students))
	for _, student := range c.Students {
		if nom := strings.TrimSpace(student.FullName); nom != "" {
			if connu, trouve := names.Lookup(student.Username); trouve &&
				strings.EqualFold(connu.FullName, nom) {
				student.FullName = ""
			}
		}
		allegee = append(allegee, student)
	}
	c.Students = allegee
	return c
}

// declared retire ce que le registre a révélé, pour ne garder que ce qui a été
// déclaré ici. C'est cette liste-là qui s'écrit sur le disque.
func (c Classroom) declared() Classroom {
	if len(c.derived) == 0 && len(c.borrowed) == 0 {
		return c
	}
	gardes := make([]roster.Person, 0, len(c.Students))
	for _, student := range c.Students {
		cle := strings.ToLower(student.Username)
		if c.derived[cle] {
			continue
		}
		if empruntes := c.borrowed[cle]; len(empruntes) > 0 {
			student.Also = slices.DeleteFunc(slices.Clone(student.Also), func(compte string) bool {
				return slices.ContainsFunc(empruntes, func(emprunte string) bool {
					return strings.EqualFold(emprunte, compte)
				})
			})
			if len(student.Also) == 0 {
				student.Also = nil
			}
		}
		gardes = append(gardes, student)
	}
	c.Students, c.derived, c.borrowed = gardes, nil, nil
	return c
}

// fragments associe à chaque étudiant ce qui peut le nommer dans un dépôt.
//
// La liste du groupe passe la première : c'est elle qui dit qui est inscrit.
// Le registre ne comble que ce qu'elle ne sait pas nommer — les slugs qu'un
// nom corrigé depuis ne produit plus.
func (c Classroom) fragments() known {
	connus := knownBy(c.Students)
	for fragment, person := range c.aliases {
		if _, deja := connus[fragment]; !deja {
			connus[fragment] = person
		}
	}
	return connus
}

// known rassemble ce qui, au dernier niveau d'un nom de dépôt, désigne une
// personne du groupe.
type known map[string]roster.Person

// personne retrouve à qui appartient le dernier niveau d'un nom de dépôt. La
// marque de doublon que GitHub ajoute à un nom déjà pris n'en fait pas un autre
// compte : « aleksilepaj-1 » est « aleksilepaj », et son dépôt est le sien.
func (k known) personne(fragment string) (roster.Person, bool) {
	if person, inscrit := k[strings.ToLower(fragment)]; inscrit {
		return person, true
	}
	base, marque := roster.WithoutDuplicateMarker(fragment)
	if !marque {
		return roster.Person{}, false
	}
	person, inscrit := k[strings.ToLower(base)]
	return person, inscrit
}

// knownBy rassemble ce qui peut désigner une personne au dernier niveau d'un
// nom de dépôt : le fragment tiré de son nom complet, et son compte GitHub.
//
// Le compte y figure parce qu'un dépôt ne porte pas toujours le nom complet.
// Ceux qu'on adopte d'une organisation qui n'a jamais suivi de convention
// portent souvent le compte, et les déplacer leur laisse ce qu'ils ont plutôt
// que d'exiger un nom qu'on n'a pas encore. Les reconnaître ensuite est ce qui
// permet de les rattacher à leur étudiant — et donc de les renommer le jour où
// le nom complet est retrouvé. Le nom complet l'emporte quand les deux
// désignent quelqu'un : c'est lui que la nomenclature écrit.
func knownBy(people []roster.Person) known {
	connus := make(known, 2*len(people))
	for _, identite := range identitiesOf(people) {
		// Tous ses comptes la désignent : un dépôt adopté sous l'un d'eux est
		// le sien, et le renommer lui donnera son nom.
		for _, compte := range identite.Accounts {
			connus[strings.ToLower(compte)] = identite.Person()
		}
	}
	for _, person := range people {
		if fragment, err := naming.Student(person.FullName); err == nil {
			connus[strings.ToLower(fragment)] = person
		}
	}
	return connus
}

// Has dit si un compte GitHub figure parmi les étudiants du groupe.
func (c Classroom) Has(username string) bool {
	_, trouve := c.Find(username)
	return trouve
}

// Add ajoute une personne à la liste du groupe. Un compte déjà présent est
// refusé plutôt que fondu dans la liste : ajouter quelqu'un qui y est déjà ne
// change rien, et le taire laisserait croire le contraire.
func (c Classroom) Add(person roster.Person) (Classroom, error) {
	person, err := person.Validate()
	if err != nil {
		return c, err
	}
	if _, deja := c.Find(person.Username); deja {
		return c, valid.Errorf("@%s est déjà dans « %s ».", person.Username, c.Label())
	}
	c.Students = append(append([]roster.Person(nil), c.Students...), person)
	return c, nil
}

// Find retrouve un étudiant du groupe par ce qui le désigne : son compte
// GitHub, ou à défaut son matricule.
//
// Le matricule réunit les lignes d'une même personne : c'est elle qui est
// rendue, avec tous ses comptes, et non la ligne qui portait celui qu'on
// cherchait.
//
// Le compte passe d'abord, et le matricule ne sert qu'ensuite. Sans quoi la
// seule façon de désigner quelqu'un dont on ne connaît pas encore le compte
// serait de connaître son compte — et c'est précisément à ces personnes-là
// qu'on veut en rattacher un, puisqu'une liste du collège n'en donne aucun.
func (c Classroom) Find(handle string) (roster.Person, bool) {
	identites := c.Identities()
	if position, trouvee := rang(identites, handle); trouvee {
		return identites[position].Person(), true
	}
	return roster.Person{}, false
}

// Handle rend ce qui désigne une personne auprès de Find et de Rename : son
// compte, à défaut son matricule. Vide, il ne désigne personne.
//
// C'est ce qu'une interface propose quand elle laisse choisir quelqu'un : une
// liste du collège n'a que des matricules, et les écarter, c'était écarter
// justement ceux à qui l'on veut rattacher un compte.
func Handle(person roster.Person) string {
	if compte := strings.TrimSpace(person.Username); compte != "" {
		return compte
	}
	return strings.TrimSpace(person.StudentID)
}

// ------------------------------------------------------------------- travaux

// Nature d'un travail. Elle ne se déclare pas : elle se lit dans le dernier
// niveau du nom des dépôts, qui nomme une équipe du groupe ou un étudiant.
const (
	Individual = "individuel"
	TeamWork   = "équipe"
)

// Assignment est un travail du groupe, tel que les dépôts le racontent.
type Assignment struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Repos    int    `json:"repos"`
	Students int    `json:"students"` // étudiants du groupe qui ont un dépôt
	Teams    int    `json:"teams"`    // équipes du groupe qui ont un dépôt
	Others   int    `json:"others"`   // dépôts dont le destinataire est inconnu
	// Kind vaut « équipe » dès qu'un dépôt du travail porte le nom d'une équipe
	// du groupe, « individuel » sinon.
	Kind string `json:"kind"`
	// PushedAt est le plus récent envoi de ses dépôts, à l'heure de la machine.
	PushedAt string `json:"pushed_at"`
	// Due est la date cible du travail, sous sa forme normale. Vide, le
	// travail n'a pas d'échéance et rien n'est jamais en retard.
	Due string `json:"due,omitempty"`
	// Ce qui suit vient des historiques relevés, et vaut zéro tant qu'ils ne
	// l'ont pas été. « Seen » dit combien de dépôts ont été regardés : sans
	// lui, un travail dont rien n'a été lu ne se distinguerait pas d'un
	// travail que personne n'a commencé.
	Seen int `json:"seen"`
	// Commits est le nombre de commits des dépôts relevés.
	Commits int `json:"commits"`
	// Late compte les dépôts dont le dernier commit suit la date cible.
	Late int `json:"late"`
	// Silent compte les dépôts où quelqu'un de visé n'a rien remis.
	Silent int `json:"silent"`
}

// ForTeams dit si le travail est distribué aux équipes.
func (a Assignment) ForTeams() bool { return a.Kind == TeamWork }

// Assignments retrouve les travaux du groupe parmi les dépôts de l'organisation.
// La nomenclature courante se relit sans rien deviner : un dépôt est du
// groupe, ou il ne l'est pas.
//
// Les équipes du groupe sont données parce qu'elles vivent sur GitHub, pas dans
// un fichier local : c'est en confrontant le dernier niveau d'un nom de dépôt à
// leurs noms qu'on sait si le travail est d'équipe. Une liste vide n'est pas
// une erreur — le groupe n'a alors que des travaux individuels.
func (c Classroom) Assignments(repos []groups.RepoInfo, equipes []teams.Team) []Assignment {
	connus := c.fragments()
	nomsDEquipes := teamFragments(equipes)
	parNom := map[string]*Assignment{}

	for _, repo := range repos {
		parts, reconnu := naming.Parse(repo.Name)
		if !reconnu || !naming.Belongs(parts, c.Session, c.Course, c.Group) {
			continue
		}
		cle := strings.ToLower(parts.Assignment)
		travail, deja := parNom[cle]
		if !deja {
			travail = &Assignment{
				ID:   naming.AssignmentID(c.Session, c.Course, c.Group, parts.Assignment),
				Name: parts.Assignment, Kind: Individual,
				Due: c.DueOf(parts.Assignment),
			}
			parNom[cle] = travail
		}
		travail.Repos++
		_, inscrit := connus[strings.ToLower(parts.Student)]
		switch {
		case nomsDEquipes[strings.ToLower(parts.Student)]:
			travail.Teams++
			travail.Kind = TeamWork
		case inscrit:
			travail.Students++
		default:
			travail.Others++
		}
		if envoi := valid.Moment(repo.PushedAt); envoi > travail.PushedAt {
			travail.PushedAt = envoi
		}
	}

	trouves := make([]Assignment, 0, len(parNom))
	for _, travail := range parNom {
		trouves = append(trouves, *travail)
	}
	sortAssignments(trouves)
	return trouves
}

// teamFragments rassemble les noms courts d'équipes, pour reconnaître le dernier
// niveau d'un nom de dépôt.
func teamFragments(equipes []teams.Team) map[string]bool {
	noms := make(map[string]bool, len(equipes))
	for _, equipe := range equipes {
		noms[strings.ToLower(equipe.Short)] = true
	}
	return noms
}

func sortAssignments(found []Assignment) {
	sort.Slice(found, func(i, j int) bool {
		if found[i].PushedAt != found[j].PushedAt {
			return found[i].PushedAt > found[j].PushedAt
		}
		return strings.ToLower(found[i].Name) < strings.ToLower(found[j].Name)
	})
}

// Recipients répartit les personnes retenues pour un travail entre celles qui
// attendent encore leur dépôt et celles qui l'ont déjà, dans l'ordre du groupe.
//
// handles restreint la distribution : chaque mot désigne une personne comme
// Find la désigne, par l'un de ses comptes ou, à défaut, par son matricule —
// c'est ainsi que se choisit qui n'a encore aucun compte. Nil, tout le groupe
// est retenu ; vide, personne : décocher tout le monde ne doit pas revenir à
// servir tout le groupe.
func (c Classroom) Recipients(assignmentID string, repos []groups.RepoInfo,
	handles []string) (waiting, served []roster.Person) {
	identites := c.Identities()
	retenues := make([]bool, len(identites))
	for _, handle := range handles {
		if position, trouvee := rang(identites, handle); trouvee {
			retenues[position] = true
		}
	}
	servies := c.servies(identites, assignmentID, repos)
	for position, identite := range identites {
		if handles != nil && !retenues[position] {
			continue
		}
		if servies[position] {
			served = append(served, identite.Person())
			continue
		}
		waiting = append(waiting, identite.Person())
	}
	return waiting, served
}

// servies dit, pour chacune de ces personnes, si un dépôt du travail est déjà
// le sien.
//
// Une personne qui travaille sous deux comptes n'a qu'un dépôt — c'est son nom
// qui le nomme, pas son compte : le trouver la tient pour servie sous chacun
// d'eux, sans quoi le second en réclamerait un autre du même nom. Le dépôt se
// rattache à sa ligne, et la ligne à sa personne par ce qui la désigne, compte
// ou matricule, jamais par un compte absent : c'est par cette clé vide que le
// dépôt d'une inscrite sans compte tenait pour servies toutes les autres.
func (c Classroom) servies(identites []Identity, assignmentID string,
	repos []groups.RepoInfo) []bool {
	parDesignation := map[string]int{}
	for position, identite := range identites {
		for _, cle := range identite.Designations() {
			parDesignation[cle] = position
		}
	}
	servies := make([]bool, len(identites))
	connus := c.fragments()
	for _, repo := range repos {
		parts, reconnu := naming.Parse(repo.Name)
		if !reconnu {
			continue
		}
		id := naming.AssignmentID(parts.Session, parts.Course, parts.Group, parts.Assignment)
		if !strings.EqualFold(id, assignmentID) {
			continue
		}
		student, inscrit := connus.personne(parts.Student)
		if !inscrit {
			continue
		}
		if position, trouvee := parDesignation[Designation(student)]; trouvee {
			servies[position] = true
		}
	}
	return servies
}

// Repos renvoie les dépôts d'un travail du groupe, du plus récent au plus ancien.
// Owned rend tous les dépôts du groupe, travaux confondus. C'est ce qui
// s'accorde à son équipe enseignante : cloisonner porte sur le groupe entier,
// pas sur un travail.
func (c Classroom) Owned(repos []groups.RepoInfo) []groups.Repo {
	trouves := make([]groups.Repo, 0)
	for _, repo := range repos {
		parts, reconnu := naming.Parse(repo.Name)
		if !reconnu || !naming.Belongs(parts, c.Session, c.Course, c.Group) {
			continue
		}
		trouves = append(trouves, groups.Repo{
			Name: repo.Name, Suffix: parts.Student,
			Private: repo.Private, URL: repo.HTMLURL,
		})
	}
	sort.Slice(trouves, func(i, j int) bool {
		return strings.ToLower(trouves[i].Name) < strings.ToLower(trouves[j].Name)
	})
	return trouves
}

func (c Classroom) Repos(assignmentID string, repos []groups.RepoInfo) []groups.Repo {
	trouves := make([]groups.Repo, 0)
	for _, repo := range repos {
		parts, reconnu := naming.Parse(repo.Name)
		if !reconnu {
			continue
		}
		id := naming.AssignmentID(parts.Session, parts.Course, parts.Group, parts.Assignment)
		if !strings.EqualFold(id, assignmentID) {
			continue
		}
		trouves = append(trouves, groups.Repo{
			Name: repo.Name, Suffix: parts.Student, Private: repo.Private,
			URL: repo.HTMLURL, PushedAt: valid.Moment(repo.PushedAt),
		})
	}
	sort.Slice(trouves, func(i, j int) bool {
		return strings.ToLower(trouves[i].Name) < strings.ToLower(trouves[j].Name)
	})
	return trouves
}

// StudentOf retrouve l'étudiant du groupe auquel un dépôt appartient. Ce qu'il
// sait des personnes vient de la liste du groupe ; « Enrich » y verse d'abord
// ce que le registre de l'organisation en dit.
func (c Classroom) StudentOf(repoName string) (roster.Person, bool) {
	parts, reconnu := naming.Parse(repoName)
	if !reconnu {
		return roster.Person{}, false
	}
	return c.fragments().personne(parts.Student)
}

// NameOf rend le nom qu'un dépôt du groupe affiche : celui de son équipe pour un
// travail d'équipe, celui de son étudiant sinon, et rien quand la liste ne le
// connaît pas.
//
// Le dernier niveau du nom de dépôt n'en tient jamais lieu. C'est un slug, et le
// lire comme un compte GitHub nommerait l'inconnu qui porte ce compte-là : une
// ligne sans nom dit au moins qu'il manque quelqu'un à la liste.
func (c Classroom) NameOf(repoName string, equipes []teams.Team) string {
	if equipe, appartient := c.TeamOf(repoName, equipes); appartient {
		return equipe.Label()
	}
	if student, inscrit := c.StudentOf(repoName); inscrit {
		return student.FullName
	}
	return ""
}

// Fragment rend le dernier niveau d'un nom de dépôt du groupe : ce qui y
// désigne une personne. Le booléen dit que le nom se lit ; il ne dit pas que
// quelqu'un se cache derrière.
func (c Classroom) Fragment(repoName string) (string, bool) {
	parts, reconnu := naming.Parse(repoName)
	if !reconnu {
		return "", false
	}
	return parts.Student, true
}

// ----------------------------------------------------------------- candidats

// StudentsOf construit une liste d'étudiants à partir de comptes GitHub seuls :
// les noms complets restent à retrouver.
func StudentsOf(usernames []string) []roster.Person {
	people := make([]roster.Person, 0, len(usernames))
	for _, login := range usernames {
		cleaned, err := valid.Login(login, "")
		if err != nil {
			continue
		}
		people = append(people, roster.Person{Username: cleaned})
	}
	return dedupe(people)
}

// dedupe écarte les doublons de comptes, en gardant le premier nom connu. Les
// comptes que la marque de doublon de GitHub avait dédoublés sont ramenés au
// leur juste avant : « aleksilepaj » et « aleksilepaj-1 » sont une personne, et
// c'est ici — dans Validate, donc à chaque enregistrement — qu'ils se
// rejoignent, quelle que soit l'interface qui a monté la liste.
func dedupe(people []roster.Person) []roster.Person {
	people = sansMarque(people, comptesDe(people))
	vus := map[string]int{}
	uniques := make([]roster.Person, 0, len(people))
	for _, person := range people {
		person.Username = strings.TrimSpace(person.Username)
		person.FullName = strings.TrimSpace(person.FullName)
		cle := Designation(person)
		if cle == "" {
			continue
		}
		if position, connu := vus[cle]; connu {
			if uniques[position].FullName == "" && person.FullName != "" {
				uniques[position].FullName = person.FullName
			}
			// Le matricule non plus ne se perd pas : c'est lui qui réunira
			// cette ligne avec les autres comptes de la même personne.
			if uniques[position].StudentID == "" && person.StudentID != "" {
				uniques[position].StudentID = person.StudentID
			}
			continue
		}
		vus[cle] = len(uniques)
		uniques = append(uniques, person)
	}
	return uniques
}

// Designation dit ce qui, dans une liste de classe, désigne une personne :
// « @ » et son compte, ou « # » et son matricule. Deux désignations égales sont
// une même inscription ; un compte et un matricule ne se confondent jamais,
// même écrits pareil.
//
// Le compte GitHub s'impose quand il est là. À défaut, le matricule suffit :
// c'est lui qui identifie vraiment quelqu'un, et une liste sortie d'Omnivox n'a
// que lui — l'écarter revenait à jeter la cohorte entière au moment de
// l'enregistrer, en silence.
//
// Une ligne qui n'a ni l'un ni l'autre, en revanche, ne peut être rapprochée de
// rien. Deux homonymes y seraient confondus, et leur donner un seul dépôt pour
// deux est précisément ce que le refus des homonymes empêche ailleurs.
func Designation(person roster.Person) string {
	if compte := strings.TrimSpace(person.Username); compte != "" {
		return "@" + strings.ToLower(compte)
	}
	if matricule := strings.TrimSpace(person.StudentID); matricule != "" {
		return "#" + strings.ToLower(matricule)
	}
	return ""
}

// designationsDe rend tout ce qui désigne une personne, sous la forme de
// Designation : chacun de ses comptes, puis son matricule. Une seule ne suffit
// pas à la retrouver : une personne inscrite deux fois — sa ligne du collège,
// puis son compte — est désignée autrement par chacune de ses lignes.
func designationsDe(person roster.Person) []string {
	comptes := person.Accounts()
	cles := make([]string, 0, len(comptes)+1)
	for _, compte := range comptes {
		cles = append(cles, Designation(roster.Person{Username: compte}))
	}
	if cle := Designation(roster.Person{StudentID: person.StudentID}); cle != "" {
		cles = append(cles, cle)
	}
	return cles
}

// sansMarque ramène à leur compte les personnes qu'une marque de doublon a fait
// dévier. La marque ne suffit pas à conclure — « LT-9 » est un vrai compte : le
// compte sans elle doit être attesté par « comptes », qui rend le nom complet
// qu'on lui connaît. Deux noms complets qui se contredisent restent deux
// personnes, quitte à laisser la marque à qui saura trancher.
func sansMarque(people []roster.Person, comptes map[string]string) []roster.Person {
	corriges := make([]roster.Person, 0, len(people))
	for _, person := range people {
		base, marque := roster.WithoutDuplicateMarker(person.Username)
		nom, atteste := comptes[strings.ToLower(base)]
		if marque && atteste && roster.SameName(nom, person.FullName) {
			person.Username = base
			if strings.TrimSpace(person.FullName) == "" {
				person.FullName = nom
			}
		}
		corriges = append(corriges, person)
	}
	return corriges
}

// comptesDe rend « compte → nom complet » pour une liste de personnes. Le nom
// le plus renseigné l'emporte : c'est lui qui dira si deux fiches sont celles
// d'une même personne.
func comptesDe(people []roster.Person) map[string]string {
	comptes := make(map[string]string, len(people))
	for _, person := range people {
		login := strings.ToLower(strings.TrimSpace(person.Username))
		if login == "" {
			continue
		}
		// Une entrée absente vaut un nom vide : dans les deux cas, le premier
		// nom renseigné qui passe l'emporte.
		if comptes[login] == "" {
			comptes[login] = strings.TrimSpace(person.FullName)
		}
	}
	return comptes
}

// ------------------------------------------------------------------- places

// NormalizeScope met une place sous une forme comparable : GitHub ne distingue
// pas la casse dans un nom de dépôt, la hiérarchie non plus.
func NormalizeScope(scope string) string {
	return strings.ToLower(strings.TrimSpace(scope))
}

// AtScope compose le groupe qui occupe une place, sans rien savoir de plus. Un
// groupe n'a pas besoin d'avoir été déclaré pour s'ouvrir : sa place est écrite
// dans le nom de chacun de ses dépôts, et c'est elle qui l'identifie.
func AtScope(org, scope string, defauts Defaults) (Classroom, error) {
	scope = strings.TrimSpace(scope)
	if scope == "" {
		return Classroom{}, valid.Errorf("Aucune place indiquée.")
	}
	niveaux := strings.Split(scope, naming.Separator)
	if len(niveaux) != naming.Levels-2 ||
		niveaux[0] == "" || niveaux[1] == "" || niveaux[2] == "" {
		return Classroom{}, valid.Errorf(
			"« %s » n'est pas une place : trois niveaux sont attendus, "+
				"« session%[2]scours%[2]sgroupe ».", scope, naming.Separator)
	}
	cours := Classroom{
		Org: org, Defaults: defauts,
		Session: niveaux[0], Course: niveaux[1], Group: niveaux[2],
	}
	return cours.Validate()
}

// Of retrouve, parmi des groupes, celui auquel un dépôt appartient : sa place
// est écrite dans son nom. Un préfixe de cours ou de session réunit les dépôts
// de plusieurs groupes, et chacun doit se nommer et se juger par le sien — sa
// liste, ses équipes, les échéances de ses travaux.
func Of(groupes []Classroom, repoName string) (Classroom, bool) {
	parts, reconnu := naming.Parse(repoName)
	if !reconnu {
		return Classroom{}, false
	}
	for _, cours := range groupes {
		if naming.Belongs(parts, cours.Session, cours.Course, cours.Group) {
			return cours, true
		}
	}
	return Classroom{}, false
}

// Places énumère les groupes que les dépôts dessinent d'eux-mêmes. Seule la
// nomenclature courante se lit sans rien deviner : les noms qui ne la suivent
// pas restent des propositions, pas des groupes.
func Places(repos []groups.RepoInfo) []string {
	vues := map[string]string{}
	for _, repo := range repos {
		parts, reconnu := naming.Parse(repo.Name)
		if !reconnu {
			continue
		}
		prefixe := naming.Prefix(parts.Session, parts.Course, parts.Group)
		vues[NormalizeScope(prefixe)] = prefixe
	}
	trouvees := make([]string, 0, len(vues))
	for _, prefixe := range vues {
		trouvees = append(trouvees, prefixe)
	}
	sort.Strings(trouvees)
	return trouvees
}

// Rename remplace la fiche d'un étudiant du groupe : son nom complet, son
// compte GitHub, ou les deux. C'est la seule façon de corriger une inscription
// sans repasser par le fichier — remplacer la liste entière pour un accent
// oublié touchait tout le monde.
//
// Le compte reste ce qui identifie la personne : le changer est accepté, mais
// pas vers un compte que quelqu'un d'autre occupe déjà.
func (c Classroom) Rename(username string, person roster.Person) (Classroom, error) {
	person, err := person.Validate()
	if err != nil {
		return c, err
	}
	position := -1
	for index, student := range c.Students {
		if student.Owns(username) {
			position = index
			break
		}
	}
	// À défaut d'un compte, le matricule désigne la ligne. C'est le seul moyen
	// d'atteindre quelqu'un qu'une liste du collège a inscrit sans compte.
	if position < 0 {
		if matricule := strings.TrimSpace(username); matricule != "" {
			for index, student := range c.Students {
				if strings.EqualFold(strings.TrimSpace(student.StudentID), matricule) {
					position = index
					break
				}
			}
		}
	}
	if position < 0 {
		return c, valid.Errorf("@%s n'est pas dans « %s ».", strings.TrimSpace(username), c.Label())
	}
	// Aucun de ses comptes ne peut être celui de quelqu'un d'autre : ce serait
	// donner à deux personnes les mêmes dépôts.
	for index, student := range c.Students {
		if index == position {
			continue
		}
		for _, compte := range person.Accounts() {
			if student.Owns(compte) {
				return c, valid.Errorf("@%s est déjà dans « %s ».", compte, c.Label())
			}
		}
	}
	c.Students = append([]roster.Person(nil), c.Students...)
	c.Students[position] = person
	return c, nil
}
