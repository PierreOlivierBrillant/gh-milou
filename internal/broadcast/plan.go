package broadcast

import (
	"bytes"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/PierreOlivierBrillant/gh-milou/internal/classroom"
	"github.com/PierreOlivierBrillant/gh-milou/internal/groups"
	"github.com/PierreOlivierBrillant/gh-milou/internal/naming"
	"github.com/PierreOlivierBrillant/gh-milou/internal/roster"
	"github.com/PierreOlivierBrillant/gh-milou/internal/starter"
	"github.com/PierreOlivierBrillant/gh-milou/internal/teams"
	"github.com/PierreOlivierBrillant/gh-milou/internal/valid"
)

// Recipient est le destinataire d'un dépôt : un étudiant, une équipe, ou
// personne de connu.
type Recipient struct {
	Repo   string
	Person roster.Person
	// Team est le nom court de l'équipe ; vide pour un travail individuel.
	Team string
}

// Label nomme le destinataire pour l'affichage.
func (r Recipient) Label() string {
	switch {
	case r.Team != "":
		return "Équipe " + r.Team
	case strings.TrimSpace(r.Person.FullName) != "":
		return r.Person.FullName
	case r.Person.Username != "":
		return "@" + r.Person.Username
	}
	return ""
}

// ForClassroom rassemble ce qu'il faut pour remplir le gabarit dans chaque
// dépôt d'un travail d'un groupe.
func ForClassroom(cours classroom.Classroom, assignmentID string, equipes []teams.Team,
	repos []groups.Repo, today time.Time) (Context, []Recipient) {
	context := Context{
		Session: cours.Session, SessionName: cours.SessionName(),
		Course: cours.Course, Group: cours.Group,
		Assignment: cours.ShortName(assignmentID), Due: cours.DueOf(assignmentID),
		Today: today,
	}
	recipients := make([]Recipient, 0, len(repos))
	for _, repo := range repos {
		recipient := Recipient{Repo: repo.Name}
		if equipe, appartient := cours.TeamOf(repo.Name, equipes); appartient {
			recipient.Team = equipe.Short
		} else if person, inscrit := cours.StudentOf(repo.Name); inscrit {
			recipient.Person = person
		}
		recipients = append(recipients, recipient)
	}
	return context, recipients
}

// ForPrefix fait de même pour un préfixe qui ne dit pas à quel groupe il
// appartient — un travail de l'ancienne nomenclature. Le groupe n'y est pas
// connu, mais le dernier niveau du nom y est le compte GitHub : {compte} garde
// donc un sens, et les champs du groupe restent vides.
func ForPrefix(prefix string, repos []groups.Repo, today time.Time) (Context, []Recipient) {
	recipients := make([]Recipient, 0, len(repos))
	for _, repo := range repos {
		recipient := Recipient{Repo: repo.Name}
		// Sous la nomenclature courante, le dernier niveau est un nom
		// slugifié, pas un compte : le prendre pour un compte l'inventerait.
		if _, courant := naming.Parse(repo.Name); !courant {
			recipient.Person.Username = repo.Suffix
		}
		recipients = append(recipients, recipient)
	}
	return Context{Assignment: prefix, Today: today}, recipients
}

// Batch rassemble des dépôts qui partagent un contexte : ceux d'un même travail
// d'un même groupe.
type Batch struct {
	Context    Context
	Recipients []Recipient
}

// ForRepos répartit des dépôts qui relèvent de plusieurs travaux — un groupe
// entier, un cours — en lots qui partagent chacun un contexte. Chaque dépôt
// reçoit ainsi les champs du sien : le nom de son travail, son échéance, son
// groupe, et la personne qu'il vise. « equipes » rend les équipes d'un groupe :
// un même nom court en désigne une dans chacun.
//
// Les dépôts hors nomenclature forment un dernier lot, au contexte du préfixe :
// c'est tout ce qu'on sait d'eux.
func ForRepos(prefix string, groupes []classroom.Classroom,
	equipes func(classroom.Classroom) []teams.Team, repos []groups.Repo, today time.Time) []Batch {
	type travail struct {
		cours  classroom.Classroom
		id     string
		depots []groups.Repo
	}
	var ordre []string
	travaux := map[string]*travail{}
	var reste []groups.Repo
	for _, repo := range repos {
		cours, place := classroom.Of(groupes, repo.Name)
		parts, _ := naming.Parse(repo.Name)
		if !place {
			reste = append(reste, repo)
			continue
		}
		id := cours.AssignmentID(parts.Assignment)
		cle := strings.ToLower(id)
		if _, vu := travaux[cle]; !vu {
			travaux[cle] = &travail{cours: cours, id: id}
			ordre = append(ordre, cle)
		}
		travaux[cle].depots = append(travaux[cle].depots, repo)
	}
	lots := make([]Batch, 0, len(ordre)+1)
	for _, cle := range ordre {
		lot := travaux[cle]
		context, recipients := ForClassroom(lot.cours, lot.id, equipes(lot.cours), lot.depots, today)
		lots = append(lots, Batch{Context: context, Recipients: recipients})
	}
	if len(reste) > 0 {
		context, recipients := ForPrefix(prefix, reste, today)
		lots = append(lots, Batch{Context: context, Recipients: recipients})
	}
	return lots
}

// Source est le fichier à déposer, tel qu'on l'a lu.
type Source struct {
	// Name est le nom du fichier, sans dossier : c'est la destination proposée.
	Name    string
	Content []byte
}

// Load lit un fichier local.
func Load(chemin string) (Source, error) {
	expanded, err := roster.ExpandPath(chemin)
	if err != nil {
		return Source{}, err
	}
	info, err := os.Stat(expanded)
	if err != nil {
		return Source{}, valid.Errorf("Fichier introuvable : %s", expanded)
	}
	if info.IsDir() {
		return Source{}, valid.Errorf("« %s » est un dossier : un seul fichier se dépose ainsi.",
			expanded)
	}
	if info.Size() > starter.MaxFileBytes {
		return Source{}, tooLarge(int(info.Size()))
	}
	content, err := os.ReadFile(expanded)
	if err != nil {
		return Source{}, valid.Errorf("Fichier illisible : %v", err)
	}
	return Source{Name: filepath.Base(expanded), Content: content}, nil
}

func tooLarge(size int) error {
	return valid.Errorf("Fichier trop lourd : %s, pour %s au plus.",
		starter.HumanSize(size), starter.HumanSize(starter.MaxFileBytes))
}

// IsText dit si un contenu est du texte : c'est lui seul qui reçoit le gabarit.
// Remplacer des octets qui ressemblent à « {cours} » au milieu d'une image la
// corromprait.
func IsText(content []byte) bool {
	return utf8.Valid(content) && !bytes.Contains(content, []byte{0})
}

// CleanPath met en forme un chemin de destination dans le dépôt, ou le refuse.
//
// Le chemin est celui d'un dépôt Git : ses séparateurs sont des « / », quelle
// que soit la machine. Une barre oblique inversée tapée sous Windows en est
// une aussi — aucun nom de fichier raisonnable n'en contient.
func CleanPath(value string) (string, error) {
	texte := strings.ReplaceAll(strings.TrimSpace(value), `\`, "/")
	if texte == "" {
		return "", valid.Errorf("Chemin dans le dépôt : il est vide.")
	}
	// « C:/… » se reconnaît sur toutes les plateformes : un chemin Windows
	// collé depuis Linux n'en est pas moins absolu.
	if strings.HasPrefix(texte, "/") || (len(texte) > 1 && texte[1] == ':') {
		return "", valid.Errorf(
			"Chemin dans le dépôt : « %s » doit être relatif à la racine du dépôt.", texte)
	}
	if strings.HasSuffix(texte, "/") {
		return "", valid.Errorf("Chemin dans le dépôt : « %s » désigne un dossier.", texte)
	}
	for _, niveau := range strings.Split(texte, "/") {
		switch {
		case niveau == "..":
			return "", valid.Errorf(
				"Chemin dans le dépôt : « %s » sort du dépôt.", texte)
		case strings.EqualFold(niveau, ".git"):
			return "", valid.Errorf(
				"Chemin dans le dépôt : « .git » appartient à Git, pas au dépôt.")
		}
	}
	nettoye := strings.TrimPrefix(path.Clean(texte), "./")
	if nettoye == "." || nettoye == "" {
		return "", valid.Errorf("Chemin dans le dépôt : « %s » ne nomme aucun fichier.", texte)
	}
	return nettoye, nil
}

// Request est ce qu'on demande de déposer.
type Request struct {
	Content []byte
	// Path est le chemin dans le dépôt ; il accepte les champs du gabarit.
	Path string
	// Message est celui du commit ; il accepte les champs du gabarit. Vide,
	// il dit quel fichier le commit ajoute.
	Message string
	// Raw dépose le contenu tel quel, sans remplir de champ : un fichier de
	// code dont les accolades ressemblent à des champs.
	Raw bool
}

// Item est le fichier tel qu'il arrivera dans un dépôt.
type Item struct {
	Recipient
	Path    string
	Content []byte
	Message string
	// Empty nomme les champs employés qui n'ont pas de valeur pour ce dépôt :
	// un étudiant absent de la liste n'a pas de nom, un travail sans date
	// cible pas d'échéance.
	Empty []string
}

// Plan est ce qu'un dépôt ferait, dépôt par dépôt.
type Plan struct {
	Items []Item
	// Templated dit que le contenu a reçu le gabarit ; faux pour un fichier
	// binaire ou une demande brute, où seuls le chemin et le message le
	// reçoivent.
	Templated bool
	// Used nomme les champs employés, Unknown ceux qui n'en sont pas et
	// restent tels quels.
	Used    []string
	Unknown []string
}

// Prepare remplit le gabarit pour chaque dépôt d'un même travail.
func Prepare(request Request, context Context, recipients []Recipient) (*Plan, error) {
	return PrepareAll(request, []Batch{{Context: context, Recipients: recipients}})
}

// PrepareAll remplit le gabarit pour des dépôts de plusieurs travaux, chacun
// avec le contexte de son lot.
func PrepareAll(request Request, lots []Batch) (*Plan, error) {
	if len(request.Content) > starter.MaxFileBytes {
		return nil, tooLarge(len(request.Content))
	}
	total := 0
	for _, lot := range lots {
		total += len(lot.Recipients)
	}
	if total == 0 {
		return nil, valid.Errorf("Aucun dépôt où déposer le fichier.")
	}
	if _, err := CleanPath(request.Path); err != nil {
		return nil, err
	}
	gabarit := string(request.Content)
	templated := !request.Raw && IsText(request.Content)
	textes := []string{request.Path, request.Message}
	if templated {
		textes = append(textes, gabarit)
	}
	plan := &Plan{Templated: templated, Used: Used(textes...), Unknown: Unknown(textes...)}

	for _, lot := range lots {
		if err := plan.fill(request, gabarit, lot); err != nil {
			return nil, err
		}
	}
	return plan, nil
}

// fill ajoute au plan les dépôts d'un lot, remplis de son contexte.
func (p *Plan) fill(request Request, gabarit string, lot Batch) error {
	for _, recipient := range lot.Recipients {
		values := lot.Context.Values(recipient)
		chemin, err := CleanPath(Fill(request.Path, values))
		if err != nil {
			return valid.Errorf("%s — dans « %s ».", strings.TrimSuffix(err.Error(), "."),
				recipient.Repo)
		}
		item := Item{Recipient: recipient, Path: chemin, Content: request.Content}
		if p.Templated {
			item.Content = []byte(Fill(gabarit, values))
		}
		item.Message = strings.TrimSpace(Fill(request.Message, values))
		if item.Message == "" {
			item.Message = "Ajoute " + chemin
		}
		for _, champ := range p.Used {
			if values[champ] == "" {
				item.Empty = append(item.Empty, champ)
			}
		}
		p.Items = append(p.Items, item)
	}
	return nil
}

// Incomplete compte les dépôts où un champ employé reste vide.
func (p *Plan) Incomplete() int {
	total := 0
	for _, item := range p.Items {
		if len(item.Empty) > 0 {
			total++
		}
	}
	return total
}
