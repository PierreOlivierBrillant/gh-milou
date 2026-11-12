package classroom

import (
	"strings"
	"time"

	"github.com/PierreOlivierBrillant/gh-milou/internal/groups"
	"github.com/PierreOlivierBrillant/gh-milou/internal/identity"
	"github.com/PierreOlivierBrillant/gh-milou/internal/naming"
	"github.com/PierreOlivierBrillant/gh-milou/internal/roster"
	"github.com/PierreOlivierBrillant/gh-milou/internal/teams"
	"github.com/PierreOlivierBrillant/gh-milou/internal/valid"
)

// La date cible d'un travail est ce qu'aucun dépôt ne peut dire. Leurs noms
// portent la place, le travail et la personne ; rien n'y porte une échéance, et
// GitHub n'a pas d'endroit où la mettre — ni jalon ni étiquette ne s'applique à
// trente dépôts à la fois.
//
// Elle vit donc là où vivent déjà les noms que les dépôts ne disent pas : dans
// le registre de l'organisation. Ce paquet ne la retient pas, il la demande —
// « Schedule » est l'une des trois questions qu'il pose au registre, avec
// « Lookup », qui dit qui se cache derrière un nom, et « Teaches », qui dit qui
// enseigne. Rien d'autre du registre ne le concerne.
//
// Une date fixée ici vaut donc pour l'équipe entière et suit d'un poste à
// l'autre, comme le nom derrière un compte.

// Schedule répond à la question : quand ce travail est-il à remettre ?
//
// Le registre de l'organisation la porte, et c'est tout ce que ce paquet a
// besoin de savoir de lui. En dépendre entièrement le lierait au client GitHub
// et au cache, dont il n'a que faire.
type Schedule interface {
	// Due rend la date cible d'un travail, désigné par son identifiant
	// complet, ou une chaîne vide s'il n'en a pas.
	Due(assignmentID string) string
}

// DueOf rend la date cible d'un travail, par son nom court ou par son
// identifiant complet — les deux désignent le même travail, et l'appelant ne
// devrait pas avoir à choisir.
//
// Un groupe dont on n'a pas versé le calendrier ne répond rien : c'est le cas
// d'un registre illisible, et il vaut mieux ne rien dire que de faire croire à
// un travail sans échéance.
func (c Classroom) DueOf(assignment string) string {
	if c.horaire == nil {
		return ""
	}
	nom := strings.TrimSpace(c.ShortName(strings.TrimSpace(assignment)))
	if nom == "" {
		return ""
	}
	return c.horaire.Due(c.AssignmentID(nom))
}

// Scheduling verse dans le groupe le calendrier de l'organisation. Comme
// « Enrich », il ne retient rien : il branche le groupe sur ce que le registre
// sait, et c'est le registre qui reste la source.
func (c Classroom) Scheduling(horaire Schedule) Classroom {
	c.horaire = horaire
	return c
}

// Deadline dit ce qu'un travail doit devenir : l'identifiant sous lequel son
// échéance se range, et la date qu'il prend. Une date vide la retire.
//
// C'est le vocabulaire dans lequel ce paquet décrit ses intentions ; les
// écrire revient à l'interface qui tient le registre. Rendre ici un changement
// de registre lierait la notion de groupe au client GitHub et au cache, dont
// elle n'a que faire.
type Deadline struct {
	// Assignment est l'identifiant complet du travail — « a26.5n6.01.tp1 ».
	// Le nom court ne suffirait pas : le registre porte toute l'organisation,
	// et deux groupes y ont chacun leur « tp1 ».
	Assignment string
	Due        string
}

// SetDue compose ce qu'il faut écrire pour fixer, changer ou retirer la date
// cible d'un travail du groupe. La date est mise en forme au passage : une date
// mal écrite s'arrête ici, pas au milieu d'une écriture.
//
// Une date vide retire l'échéance : il n'y a pas de geste séparé pour cela,
// c'est la même décision prise dans l'autre sens.
func (c Classroom) SetDue(assignment, due string) ([]Deadline, error) {
	nom := strings.TrimSpace(c.ShortName(strings.TrimSpace(assignment)))
	if nom == "" {
		return nil, valid.Errorf("Date cible : aucun travail indiqué.")
	}
	normale, err := valid.NormalizeDue(due)
	if err != nil {
		return nil, err
	}
	return []Deadline{{Assignment: c.AssignmentID(nom), Due: normale}}, nil
}

// RenameDue fait suivre à un travail renommé la date qu'il avait. Sans cela,
// corriger un nom ferait disparaître son échéance sans que rien ne le dise :
// elle est rangée sous l'identifiant du travail, et il vient de changer.
//
// Rien n'est rendu quand il n'y a rien à reporter.
func (c Classroom) RenameDue(from, to string) []Deadline {
	due := c.DueOf(from)
	if due == "" {
		return nil
	}
	return []Deadline{
		{Assignment: c.AssignmentID(c.ShortName(from))},
		{Assignment: c.AssignmentID(strings.TrimSpace(to)), Due: due},
	}
}

// MoveDue fait suivre à un groupe d'arrivée les dates cibles des travaux qui le
// rejoignent, et les retire de leur identifiant de départ.
//
// Un travail déplacé garde son échéance : c'est le même travail, rangé
// ailleurs. La date est rangée sous l'identifiant qu'il prend à l'arrivée, qui
// porte la place du groupe d'arrivée et le nom qu'on lui donne au passage.
func MoveDue(depart, arrivee Classroom, travaux []Relocation) []Deadline {
	var mouvements []Deadline
	for _, demande := range travaux {
		due := depart.DueOf(demande.ID)
		if due == "" {
			continue
		}
		nom := strings.TrimSpace(demande.Name)
		if nom == "" {
			nom = depart.ShortName(demande.ID)
		}
		mouvements = append(mouvements,
			Deadline{Assignment: depart.AssignmentID(depart.ShortName(demande.ID))},
			Deadline{Assignment: arrivee.AssignmentID(nom), Due: due})
	}
	return mouvements
}

// ------------------------------------------------------------ ce qu'on remet

// Review est ce qu'un dépôt raconte d'une remise, une fois confronté à la date
// cible et aux personnes qu'il vise.
type Review struct {
	Repo    string `json:"repo"`
	Commits int    `json:"commits"`
	// Last date la remise : c'est le commit le plus récent qui n'est pas d'un
	// enseignant, au format RFC 3339. Vide, rien n'y a été remis.
	Last string `json:"last,omitempty"`
	// Late dit qu'un commit suit la date cible. Sans date cible, jamais.
	Late bool `json:"late"`
	// Silent nomme les personnes visées par le dépôt dont aucun commit ne
	// porte la trace. Un dépôt d'équipe en nomme plusieurs ; un dépôt
	// individuel, au plus une.
	Silent []roster.Person `json:"silent,omitempty"`
	// Strangers nomme les comptes qui ont commis dans le dépôt sans qu'il les
	// vise ni qu'ils enseignent. Le plus souvent c'est la personne visée sous
	// un autre compte — un courriel resté configuré sur un vieux compte, et
	// GitHub attribue le commit à ce compte-là, quel que soit celui qui a
	// poussé ; parfois c'est quelqu'un d'autre. Rien n'en est déduit : c'est à
	// qui enseigne de réunir les deux comptes, ou non, et il faut d'abord
	// qu'il le voie.
	Strangers []string `json:"strangers,omitempty"`
}

// Missing dit qu'au moins une personne visée n'a rien remis.
func (r Review) Missing() bool { return len(r.Silent) > 0 }

// Targets rend les personnes qu'un dépôt vise : l'étudiant à qui il appartient,
// ou les membres de l'équipe quand le travail est d'équipe.
//
// C'est cette liste que le silence confronte. Elle peut être vide — un dépôt
// hors liste, une équipe dont on ne connaît aucun membre —, et rien ne s'en
// déduit alors : on ne reproche pas son silence à quelqu'un qu'on ne connaît
// pas.
func (c Classroom) Targets(repoName string, equipes []teams.Team) []roster.Person {
	if equipe, appartient := c.TeamOf(repoName, equipes); appartient {
		return c.Members(equipe)
	}
	if student, inscrit := c.StudentOf(repoName); inscrit {
		return []roster.Person{student}
	}
	return nil
}

// HandedIn date la remise d'un dépôt : le commit le plus récent qui n'est pas
// d'un enseignant.
//
// Un gabarit poussé à l'ouverture du travail, une correction déposée après
// coup, une note ajoutée une fois l'échéance passée sont l'œuvre de qui
// enseigne. Les compter daterait la remise du jour où l'enseignant y a touché,
// et mettrait l'étudiant en retard pour cela.
//
// Un groupe qu'on n'a pas branché sur le registre — « Staffing » — ne sait pas
// qui enseigne : la date du dernier commit est alors tout ce qu'il a.
func (c Classroom) HandedIn(remise groups.Handin) string {
	if c.enseignants == nil {
		return remise.Last
	}
	return remise.LastBut(c.enseignants.Teaches)
}

// StudentCommits compte les commits d'un dépôt qui sont une remise : ceux de
// qui enseigne n'en sont pas, pour la même raison qu'ils ne datent pas la
// remise (voir HandedIn). Sans savoir qui enseigne, tous comptent.
func (c Classroom) StudentCommits(remise groups.Handin) int {
	if c.enseignants == nil {
		return remise.Commits
	}
	return remise.CommitsBut(c.enseignants.Teaches)
}

// Review confronte ce qu'un dépôt a reçu à ce qu'on en attendait.
func (c Classroom) Review(repoName string, remise groups.Handin, due time.Time,
	equipes []teams.Team) Review {
	bilan := Review{Repo: repoName, Commits: c.StudentCommits(remise), Last: c.HandedIn(remise)}
	if !due.IsZero() && bilan.Last != "" {
		if dernier, err := time.Parse(time.RFC3339, bilan.Last); err == nil {
			bilan.Late = dernier.After(due)
		}
	}
	cibles := c.Targets(repoName, equipes)
	for _, personne := range cibles {
		if remise.By(personne.Accounts()) > 0 || remise.Signed(personne.FullName) {
			continue
		}
		bilan.Silent = append(bilan.Silent, personne)
	}
	// Sans savoir qui enseigne, un étranger ne se distingue pas d'un
	// enseignant : mieux vaut ne rien dire que prendre l'un pour l'autre. Un
	// dépôt que rien ne vise n'a pas d'étrangers non plus : tout le monde y est
	// inattendu, et cela ne dit rien.
	if c.enseignants != nil && len(cibles) > 0 {
		attendus := make([]string, 0, len(cibles))
		for _, personne := range cibles {
			attendus = append(attendus, personne.Accounts()...)
		}
		bilan.Strangers = remise.Strangers(attendus, c.enseignants.Teaches)
	}
	return bilan
}

// ReviewOf confronte un dépôt à l'échéance de son propre travail, celle que son
// nom désigne. C'est ce qu'il faut à une liste qui mêle plusieurs travaux — un
// groupe entier, un cours — : chaque dépôt n'y est en retard que sur la date
// qui le concerne, comme sur la page de son travail. Un dépôt d'un autre
// groupe n'a pas d'échéance ici, même si son travail porte le même nom court.
func (c Classroom) ReviewOf(repoName string, remise groups.Handin,
	equipes []teams.Team) Review {
	var due time.Time
	if parts, reconnu := naming.Parse(repoName); reconnu &&
		naming.Belongs(parts, c.Session, c.Course, c.Group) {
		due, _ = valid.ParseDue(c.DueOf(parts.Assignment))
	}
	return c.Review(repoName, remise, due, equipes)
}

// Reviews confronte tous les dépôts d'un travail. Les dépôts dont l'historique
// n'a pas été relevé sont absents du résultat : l'écran doit pouvoir distinguer
// « rien remis » de « pas encore regardé ».
func (c Classroom) Reviews(assignmentID string, repos []groups.RepoInfo,
	equipes []teams.Team, remises map[string]groups.Handin) []Review {
	due, _ := valid.ParseDue(c.DueOf(assignmentID))
	bilans := make([]Review, 0, len(remises))
	for _, depot := range c.Repos(assignmentID, repos) {
		remise, releve := remises[depot.Name]
		if !releve {
			continue
		}
		bilans = append(bilans, c.Review(depot.Name, remise, due, equipes))
	}
	return bilans
}

// WithHandins verse dans les travaux ce que les historiques relevés en disent :
// les commits comptés, les retards et les silences.
//
// Les travaux reviennent complétés plutôt que recalculés : ce qui se lit dans
// les noms de dépôts a déjà été établi, et un relevé partiel ne doit rien y
// changer.
func (c Classroom) WithHandins(travaux []Assignment, repos []groups.RepoInfo,
	equipes []teams.Team, remises map[string]groups.Handin) []Assignment {
	if len(remises) == 0 {
		return travaux
	}
	completes := make([]Assignment, 0, len(travaux))
	for _, travail := range travaux {
		for _, bilan := range c.Reviews(travail.ID, repos, equipes, remises) {
			travail.Seen++
			travail.Commits += bilan.Commits
			if bilan.Late {
				travail.Late++
			}
			if bilan.Missing() {
				travail.Silent++
			}
		}
		completes = append(completes, travail)
	}
	return completes
}

// ------------------------------------------------------ où en est une remise

// HandinState dit où en est la remise d'un dépôt, d'un mot.
//
// Les états vivent ici plutôt que dans une interface : « en retard » doit
// vouloir dire la même chose au navigateur, au terminal et à la ligne de
// commande, et c'est aussi sur eux qu'on filtre.
type HandinState string

const (
	// AnyHandin ne retient rien : tous les états passent.
	AnyHandin HandinState = ""
	// Unread dit qu'on n'a pas regardé. Ce n'est pas un dépôt vide : c'est un
	// dépôt dont l'historique n'a pas été relevé, et rien ne s'en conclut.
	Unread HandinState = "non relevé"
	// Unaccepted dit que l'invitation n'a pas encore été acceptée. La personne
	// n'a pas pu remettre — son dépôt ne lui est pas ouvert —, et lui
	// reprocher son silence serait injuste.
	Unaccepted HandinState = "non accepté"
	// Unsent dit que rien n'a été remis.
	Unsent HandinState = "non remis"
	// Overdue dit que la remise suit la date cible.
	Overdue HandinState = "en retard"
	// Delivered dit que le travail est remis, et à temps.
	Delivered HandinState = "remis"
)

// HandinStates énumère les états, dans l'ordre où les proposer : du plus
// tranquille au plus alarmant, puis ce qu'on n'a pas encore regardé.
var HandinStates = []HandinState{
	AnyHandin, Delivered, Overdue, Unsent, Unaccepted, Unread,
}

// ParseHandinState valide un état saisi. Il se lit accentué comme non
// accentué : « non relevé » se tape rarement avec ses accents au terminal.
func ParseHandinState(value string) (HandinState, error) {
	demande := valid.Slugify(value)
	for _, candidat := range HandinStates {
		if demande == valid.Slugify(string(candidat)) {
			return candidat, nil
		}
	}
	return AnyHandin, valid.Errorf(
		"Remise : « %s » est inconnu (attendu : remis, en retard, non remis, "+
			"non accepté, non relevé, ou rien).", value)
}

// Keep dit si une remise dans cet état répond au critère. Un critère vide ne
// retient rien : il laisse simplement passer.
func (s HandinState) Keep(etat HandinState) bool {
	return s == AnyHandin || s == etat
}

// StateCount compte les dépôts d'un travail qui en sont au même point.
type StateCount struct {
	State HandinState `json:"state"`
	Count int         `json:"count"`
}

// Tally compte des dépôts par état de remise, dans l'ordre de HandinStates :
// c'est la synthèse qu'on lit d'un coup d'œil au-dessus d'une liste, du plus
// tranquille au plus alarmant. Un état qu'aucun dépôt n'a n'y figure pas.
func Tally(etats []HandinState) []StateCount {
	comptes := make(map[HandinState]int, len(HandinStates))
	for _, etat := range etats {
		comptes[etat]++
	}
	synthese := []StateCount{}
	for _, etat := range HandinStates {
		if etat != AnyHandin && comptes[etat] > 0 {
			synthese = append(synthese, StateCount{State: etat, Count: comptes[etat]})
		}
	}
	return synthese
}

// StateOf dit où en est la remise d'un dépôt.
//
// « releve » dit que l'historique a été lu. Sans lui, rien ne se conclut : un
// dépôt qu'on n'a pas regardé n'est pas un dépôt vide, et c'est précisément ce
// que l'écran doit pouvoir distinguer.
//
// « attend » dit qu'une personne visée a été invitée sans avoir encore accepté.
// Il n'explique que l'absence de remise : une équipe qui a remis a remis, même
// si l'un de ses membres n'a pas encore cliqué sur le courriel de GitHub.
func StateOf(bilan Review, releve, attend bool) HandinState {
	switch {
	case !releve:
		return Unread
	case bilan.Late:
		return Overdue
	case bilan.Last != "":
		return Delivered
	case attend:
		return Unaccepted
	default:
		return Unsent
	}
}

// Awaiting dit qu'une personne que le dépôt vise a été invitée sans avoir
// encore accepté.
//
// Les comptes invités sont donnés plutôt que cherchés ici : ils viennent des
// accès du dépôt, que ce paquet n'a pas à savoir lire. Un dépôt d'équipe
// s'ouvre autrement — c'est l'équipe GitHub qui donne l'accès, et c'est donc
// son invitation qui compte.
func (c Classroom) Awaiting(repoName string, equipes []teams.Team, invites []string) bool {
	if equipe, appartient := c.TeamOf(repoName, equipes); appartient {
		return len(equipe.Pending) > 0
	}
	for _, personne := range c.Targets(repoName, equipes) {
		for _, compte := range personne.Accounts() {
			if containsFold(invites, compte) {
				return true
			}
		}
	}
	return false
}

// InvitationOf dit où en est l'invitation de ceux qu'un dépôt vise. « acces »
// vaut nil quand ses accès n'ont pas été relevés : rien ne se conclut alors.
//
// Un dépôt d'équipe ne se lit pas dans ses accès : c'est l'équipe GitHub qui
// l'ouvre, et c'est donc l'invitation dans l'équipe qui compte. L'outil n'en
// relève que les invitations en attente, qui ne disent pas si elles ont
// expiré : l'état n'y est jamais « expirée », et rien ne s'y renvoie.
//
// Un dépôt dont on ne connaît pas le destinataire — hors liste — se juge sur
// tous ceux qui y ont accès : c'est encore la meilleure réponse à « quelqu'un
// y est-il entré ? ».
func (c Classroom) InvitationOf(repoName string, equipes []teams.Team,
	acces *identity.Access) (identity.InvitationState, identity.Invitation) {
	if equipe, appartient := c.TeamOf(repoName, equipes); appartient {
		switch {
		case len(equipe.Pending) > 0:
			return identity.InvitationPending, identity.Invitation{}
		case len(equipe.Members) > 0:
			return identity.InvitationAccepted, identity.Invitation{}
		default:
			return identity.InvitationNone, identity.Invitation{}
		}
	}
	if acces == nil {
		return identity.InvitationUnknown, identity.Invitation{}
	}
	return acces.InvitationOf(c.comptesVises(repoName, equipes))
}

// ToInvite rend les invitations à envoyer pour que ces dépôts s'ouvrent à ceux
// qu'ils visent : une neuve à la place de chaque invitation expirée, et une
// première à qui n'en a aucune, au droit donné.
//
// Seuls les dépôts dont les accès ont été relevés comptent : ne pas savoir
// n'est pas savoir que personne n'a été invité. Un dépôt d'équipe n'en reçoit
// jamais — c'est l'équipe qui l'ouvre. Et un dépôt dont on ne connaît pas le
// destinataire n'a que ses invitations expirées à renvoyer : on ne sait pas qui
// inviter pour la première fois.
func (c Classroom) ToInvite(repos []string, equipes []teams.Team,
	acces map[string]identity.Access, permission string) []identity.Dispatch {
	var envois []identity.Dispatch
	for _, repo := range repos {
		lus, inspecte := acces[repo]
		if !inspecte {
			continue
		}
		if _, appartient := c.TeamOf(repo, equipes); appartient {
			continue
		}
		lus.Repo = repo
		envois = append(envois, lus.Dispatches(c.comptesVises(repo, equipes), permission)...)
	}
	return envois
}

// comptesVises rend les comptes GitHub de ceux qu'un dépôt vise.
func (c Classroom) comptesVises(repoName string, equipes []teams.Team) []string {
	var comptes []string
	for _, personne := range c.Targets(repoName, equipes) {
		comptes = append(comptes, personne.Accounts()...)
	}
	return comptes
}
