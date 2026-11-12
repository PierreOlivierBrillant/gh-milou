// Package runner applique un plan de génération : création des dépôts, dépôt des
// fichiers de départ, puis attribution de l'accès — une invitation à la
// personne pour un travail individuel, un partage avec l'équipe pour un travail
// d'équipe. Les marques qu'il dépose en chemin montent ensuite au registre de
// l'organisation.
package runner

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/PierreOlivierBrillant/gh-milou/internal/config"
	"github.com/PierreOlivierBrillant/gh-milou/internal/ghapi"
	"github.com/PierreOlivierBrillant/gh-milou/internal/naming"
	"github.com/PierreOlivierBrillant/gh-milou/internal/plan"
	"github.com/PierreOlivierBrillant/gh-milou/internal/registry"
	"github.com/PierreOlivierBrillant/gh-milou/internal/signature"
	"github.com/PierreOlivierBrillant/gh-milou/internal/starter"
	"github.com/PierreOlivierBrillant/gh-milou/internal/valid"
)

// Statuts possibles pour un dépôt du plan.
const (
	Created  = "créé"
	Existing = "déjà présent"
	Failed   = "échec"
	Skipped  = "ignoré"
)

// Mentions portées au bilan pour les fichiers de départ et l'invitation.
const (
	StarterNone     = "non"
	StarterSkipped  = "ignoré (dépôt non vide)"
	StarterFailed   = "échec"
	CollaboratorNo  = "non"
	CollaboratorYes = "prévu"
	// TeamShared dit qu'un dépôt d'équipe a été partagé avec elle. L'accès est
	// accordé à l'équipe entière : personne n'est invité individuellement, et
	// changer sa composition suffit à changer qui voit le dépôt.
	TeamShared = "équipe"
	// CollaboratorUnknown dit qu'il n'y avait personne à inviter : la liste du
	// groupe connaît cette personne, mais pas encore son compte GitHub — c'est
	// le cas de toute cohorte importée du collège avant qu'on les rattache.
	// Le dépôt est créé, et lui seul y accède pour l'instant. La case restait
	// vide, ce qui se lit comme un oubli plutôt que comme un fait.
	CollaboratorUnknown = "compte à rattacher"
)

// Result est l'issue du traitement d'un dépôt.
type Result struct {
	Username string `json:"username"`
	FullName string `json:"full_name"`
	// StudentID est le matricule, quand la liste le donne. C'est lui qui range
	// la marque du dépôt au registre tant que la personne n'a pas de compte.
	StudentID string `json:"student_id,omitempty"`
	// Team nomme l'équipe destinataire ; vide pour un travail individuel.
	Team         string `json:"team,omitempty"`
	Repo         string `json:"repo"`
	Status       string `json:"status"`
	URL          string `json:"url"`
	Collaborator string `json:"collaborator"`
	Starter      string `json:"starter"`
	// Signature est la marque invisible déposée dans le dépôt, sous sa forme
	// lisible. Vide, le dépôt n'a pas été signé — et le champ dit alors
	// pourquoi dans Error, quand c'est un échec plutôt qu'un choix.
	Signature string `json:"signature,omitempty"`
	Error     string `json:"error"`
}

// Failed indique un traitement en échec.
func (r Result) IsFailed() bool { return r.Status == Failed }

// Report est le bilan complet d'une exécution.
type Report struct {
	Org        string   `json:"org"`
	Assignment string   `json:"assignment"`
	StartedAt  string   `json:"started_at"`
	DryRun     bool     `json:"dry_run"`
	Results    []Result `json:"results"`
	// Marks dit ce qu'il est advenu des marques déposées : une marque qui n'a
	// pas monté au registre ne désignera jamais personne, et le bilan doit
	// le dire.
	Marks Delivery `json:"marks,omitzero"`
}

// Delivery dit ce qu'il est advenu des marques d'une distribution : combien de
// dépôts ont été signés, combien de marques ont monté au registre, et pourquoi
// les autres n'y sont pas. Le terminal et le navigateur en disent la même
// phrase.
type Delivery struct {
	Signed   int `json:"signed"`
	Recorded int `json:"recorded"`
	// Problem dit pourquoi des marques n'ont pas monté au registre. Vide, elles
	// y sont toutes.
	Problem string `json:"problem,omitempty"`
}

// Count compte les résultats d'un statut donné.
func (r *Report) Count(status string) int {
	total := 0
	for _, result := range r.Results {
		if result.Status == status {
			total++
		}
	}
	return total
}

// Failures renvoie les dépôts en échec.
func (r *Report) Failures() []Result {
	var failures []Result
	for _, result := range r.Results {
		if result.IsFailed() {
			failures = append(failures, result)
		}
	}
	return failures
}

// Save écrit le bilan au format JSON et CSV, et renvoie les deux chemins.
func (r *Report) Save(directory string) (string, string, error) {
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return "", "", err
	}
	stamp := strings.NewReplacer(":", "", "-", "").Replace(r.StartedAt)
	if len(stamp) > 15 {
		stamp = stamp[:15]
	}
	base := r.Assignment
	if base == "" {
		base = "cohorte"
	}
	base = base + "-" + stamp

	jsonPath := filepath.Join(directory, base+".json")
	payload, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return "", "", err
	}
	if err := os.WriteFile(jsonPath, append(payload, '\n'), 0o600); err != nil {
		return "", "", err
	}

	csvPath := filepath.Join(directory, base+".csv")
	file, err := os.OpenFile(csvPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return "", "", err
	}
	defer file.Close()
	writer := csv.NewWriter(file)
	records := [][]string{{
		"nom_complet", "github_username", "equipe", "depot", "statut",
		"collaborateur", "fichiers_de_depart", "url", "erreur",
	}}
	for _, result := range r.Results {
		records = append(records, []string{
			result.FullName, result.Username, result.Team, result.Repo, result.Status,
			result.Collaborator, result.Starter, result.URL, result.Error,
		})
	}
	if err := writer.WriteAll(records); err != nil {
		return "", "", err
	}
	return jsonPath, csvPath, nil
}

// Signatures rend les marques délivrées, prêtes à monter au registre, pour le
// travail donné.
//
// Une marque qu'on ne verserait pas resterait dans le dépôt sans que personne
// ne puisse dire de qui elle est — la détection la verrait encore en double,
// mais s'arrêterait là. C'est pourquoi l'exécuteur les verse lui-même dès
// qu'on lui donne le registre : voir « deliver ».
//
// Chacun y est désigné comme dans sa liste de classe : par son compte, et à
// défaut par son matricule. Une inscrite sans compte — toute cohorte importée
// du collège — en reçoit donc une comme les autres, et c'est maintenant
// qu'elle doit monter : le jeton est tiré au hasard, rien ne le retrouverait
// une fois son compte rattaché. Le dépôt d'une équipe, lui, ne désigne aucun
// de ses membres : sa marque se range sous l'équipe.
func (r *Report) Signatures(assignment string) []signature.Issued {
	delivrees, _ := r.marques(assignment)
	return delivrees
}

// marques sépare les marques déposées : celles qui désignent quelqu'un, prêtes
// à monter, et les dépôts dont la marque ne désigne personne — ceux d'une
// personne qui n'a ni compte ni matricule.
func (r *Report) marques(assignment string) ([]signature.Issued, []Result) {
	delivrees := make([]signature.Issued, 0, len(r.Results))
	var anonymes []Result
	for _, result := range r.Results {
		if result.Signature == "" {
			continue
		}
		delivree := signature.Issued{
			Assignment: assignment, Token: result.Signature, IssuedAt: r.StartedAt,
		}
		switch {
		case strings.TrimSpace(result.Username) != "":
			delivree.Username = result.Username
		case strings.TrimSpace(result.StudentID) != "":
			delivree.StudentID = result.StudentID
		case strings.TrimSpace(result.Team) != "":
			delivree.Team = result.Team
		default:
			anonymes = append(anonymes, result)
			continue
		}
		delivrees = append(delivrees, delivree)
	}
	return delivrees, anonymes
}

// sansAttribution dit ce que coûte une marque qui n'a pas monté au registre.
const sansAttribution = "Les dépôts sont signés, mais rien ne dira à qui appartient une marque."

// deliver verse au registre les marques que la distribution vient de déposer,
// et dit ce qu'il en est advenu.
//
// Un échec ne fait pas échouer la distribution : les dépôts sont créés, les
// étudiants peuvent travailler. Ce qui manque est la table qui relie une marque
// à quelqu'un — la détection verra encore deux travaux porter la même, mais
// personne ne pourra dire de qui il s'agit. Cela se dit, sans rien refuser.
func (r *Report) deliver(registre *registry.Store) Delivery {
	versement := Delivery{Signed: r.Signed()}
	if versement.Signed == 0 {
		return versement
	}
	// Le registre range une marque sous son travail, et un préfixe hérité —
	// « tp1 » — ne dit pas de quel groupe il est : deux groupes qui ont
	// chacun le leur s'y confondraient. Le registre le refuserait ; mieux vaut
	// le dire en clair que de répéter son refus.
	if _, _, situe := naming.SplitAssignment(r.Assignment); !situe {
		versement.Problem = fmt.Sprintf("Marques non enregistrées au registre : il ne les "+
			"range que sous un travail nommé « session%[2]scours%[2]sgroupe%[2]stravail », "+
			"et « %[1]s » n'en est pas un. %[3]s",
			r.Assignment, naming.Separator, sansAttribution)
		return versement
	}
	if registre == nil {
		versement.Problem = "Marques non enregistrées : aucun registre ne les reçoit. " +
			sansAttribution
		return versement
	}
	delivrees, anonymes := r.marques(r.Assignment)
	if len(delivrees) > 0 {
		if _, err := registre.Apply(registry.Mark(delivrees...)); err != nil {
			// L'erreur finit souvent par un point : la phrase qui la reprend ne
			// doit pas en ajouter un second.
			versement.Problem = "Marques non enregistrées au registre : " +
				strings.TrimSuffix(strings.TrimSpace(err.Error()), ".") + ". " + sansAttribution
			return versement
		}
		versement.Recorded = len(delivrees)
	}
	if len(anonymes) > 0 {
		versement.Problem = anonymat(anonymes)
	}
	return versement
}

// anonymat dit quelles marques ne montent pas faute de rien qui désigne leur
// destinataire. Un registre range une personne sous son compte ou son
// matricule, jamais sous son nom : deux homonymes s'y confondraient.
func anonymat(anonymes []Result) string {
	noms := make([]string, 0, len(anonymes))
	for index, result := range anonymes {
		if index == 5 {
			noms = append(noms, "…")
			break
		}
		noms = append(noms, result.FullName)
	}
	return valid.Agree(len(anonymes),
		fmt.Sprintf("%s n'a ni compte ni matricule : la marque de son dépôt ne monte "+
			"pas au registre, et rien ne dira à qui elle est.", noms[0]),
		fmt.Sprintf("%s n'ont ni compte ni matricule (%s) : les marques de leurs dépôts "+
			"ne montent pas au registre, et rien ne dira à qui elles sont.",
			valid.Plural(len(anonymes), "personne"), strings.Join(noms, ", ")))
}

// Signed compte les dépôts signés.
func (r *Report) Signed() int {
	compte := 0
	for _, result := range r.Results {
		if result.Signature != "" {
			compte++
		}
	}
	return compte
}

// ProgressFunc est appelée après chaque dépôt traité.
type ProgressFunc func(index, total int, result Result)

// Options règle une exécution.
type Options struct {
	DryRun       bool
	ForceStarter bool
	// Sign dépose dans le README de chaque dépôt une marque invisible, propre à
	// son destinataire. Deux travaux qui portent la même marque n'ont pas
	// d'explication innocente ; l'absence de marque, elle, ne prouve rien.
	Sign bool
	// Registry est le registre de l'organisation, où montent les marques
	// déposées : c'est le seul endroit qui dira un jour à qui l'une d'elles
	// appartient. L'exécuteur les y verse lui-même à la fin du lot, et le
	// bilan dit ce qu'il en est advenu : un chemin qui signe ne peut plus
	// oublier de les verser, seulement de donner le registre — et cela se dit.
	Registry   *registry.Store
	OnProgress ProgressFunc
	// TeacherTeam est l'équipe enseignante du groupe, quand il est cloisonné.
	// Chaque dépôt créé lui est accordé d'emblée : sans cela, un groupe
	// cloisonné se décloisonnerait tout seul à la distribution suivante, et
	// l'enseignant ne verrait pas les dépôts qu'il vient de créer.
	//
	// Vide, rien n'est accordé : un groupe non cloisonné se comporte comme
	// avant.
	TeacherTeam string
	// TeacherPermission est le droit donné à cette équipe.
	TeacherPermission string
}

// Executor applique un plan de génération sur une organisation.
type Executor struct {
	client   *ghapi.Client
	settings config.Settings
	starter  *starter.Bundle
	sleep    func(time.Duration)
	now      func() time.Time
}

// New construit un exécuteur.
func New(client *ghapi.Client, settings config.Settings, bundle *starter.Bundle) *Executor {
	return &Executor{client: client, settings: settings, starter: bundle,
		sleep: time.Sleep, now: time.Now}
}

// WithClock remplace l'horloge et la temporisation (tests).
func (e *Executor) WithClock(sleep func(time.Duration), now func() time.Time) *Executor {
	e.sleep, e.now = sleep, now
	return e
}

// Run traite chaque dépôt du plan ; une erreur isolée n'interrompt pas le lot.
func (e *Executor) Run(items []plan.PlannedRepo, options Options) (*Report, error) {
	report := &Report{
		Org:        e.settings.Org,
		Assignment: e.settings.Assignment,
		StartedAt:  e.now().UTC().Format("2006-01-02T15:04:05Z"),
		DryRun:     options.DryRun,
	}

	var templateOwner, templateRepo string
	if strings.TrimSpace(e.settings.Template) != "" {
		owner, repo, err := valid.RepoRef(e.settings.Template)
		if err != nil {
			return nil, err
		}
		templateOwner, templateRepo = owner, repo
	}

	total := len(items)
	for index, item := range items {
		result := e.process(item, templateOwner, templateRepo, options)
		report.Results = append(report.Results, result)
		if options.OnProgress != nil {
			options.OnProgress(index+1, total, result)
		}
		// Marge entre deux créations réelles : GitHub limite les écritures en rafale.
		if result.Status == Created && !options.DryRun &&
			index+1 < total && e.settings.DelaySeconds > 0 {
			e.sleep(time.Duration(e.settings.DelaySeconds * float64(time.Second)))
		}
	}
	// Les marques montent d'un seul commit, une fois le lot traité : une par
	// dépôt ferait autant d'écritures au registre que de dépôts.
	if options.Sign && !options.DryRun {
		report.Marks = report.deliver(options.Registry)
	}
	return report, nil
}

// grantTeachers donne un dépôt à l'équipe enseignante du groupe, s'il y en a
// une. Sans équipe, il n'y a rien à accorder : le groupe n'est pas cloisonné.
func (e *Executor) grantTeachers(org, repo string, options Options) error {
	equipe := strings.TrimSpace(options.TeacherTeam)
	if equipe == "" {
		return nil
	}
	return e.client.GrantTeamRepo(org, equipe, org, repo, options.TeacherPermission)
}

func (e *Executor) process(item plan.PlannedRepo, templateOwner, templateRepo string,
	options Options) Result {
	org := e.settings.Org
	result := Result{
		Username:     item.Person.Username,
		FullName:     item.Person.FullName,
		StudentID:    item.Person.StudentID,
		Team:         item.Team,
		Repo:         item.Name,
		Status:       Skipped,
		Collaborator: CollaboratorNo,
		Starter:      StarterNone,
	}
	if item.ForTeam() {
		result.FullName = item.Recipient()
	}

	existing, err := e.client.GetRepo(org, item.Name)
	if err != nil {
		result.Status, result.Error = Failed, err.Error()
		return result
	}

	branch := "main"
	switch {
	case existing != nil:
		// Reprise possible : on ne recrée rien, mais on complète l'invitation.
		result.Status = Existing
		result.URL = existing.HTMLURL
		if existing.DefaultBranch != "" {
			branch = existing.DefaultBranch
		}
	case options.DryRun:
		result.Status = Created
		result.URL = fmt.Sprintf("https://github.com/%s/%s", org, item.Name)
		if e.settings.AddCollaborator {
			result.Collaborator = CollaboratorYes
			if !item.ForTeam() && len(comptesDe(item)) == 0 {
				result.Collaborator = CollaboratorUnknown
			}
		}
		if e.starter != nil {
			result.Starter = valid.Plural(len(e.starter.Files), "fichier prévu", "fichiers prévus")
		}
		return result
	default:
		repo, err := e.create(item, templateOwner, templateRepo)
		if err != nil {
			result.Status, result.Error = Failed, err.Error()
			return result
		}
		result.Status = Created
		result.URL = repo.HTMLURL
		if result.URL == "" {
			result.URL = fmt.Sprintf("https://github.com/%s/%s", org, item.Name)
		}
		if repo.DefaultBranch != "" {
			branch = repo.DefaultBranch
		}
		// Le dépôt vient de naître : l'équipe enseignante du groupe le reçoit
		// avant tout le reste. Un échec ici n'annule pas la création — le dépôt
		// existe —, mais il se dit : croire un groupe cloisonné alors qu'un de
		// ses dépôts ne l'est pas est pire que de le savoir.
		if err := e.grantTeachers(org, item.Name, options); err != nil {
			result.Status, result.Error = Failed, fmt.Sprintf(
				"dépôt créé mais accès de l'équipe enseignante impossible : %v", err)
			return result
		}
	}

	if e.starter != nil {
		if options.DryRun {
			result.Starter = e.previewStarter(item, branch, options.ForceStarter)
		} else if !e.applyStarter(item, &result, branch, options.ForceStarter) {
			return result
		}
	}

	// On ne signe que ce qu'on vient d'écrire : un dépôt créé à l'instant, ou
	// un dépôt dont les fichiers de départ viennent d'être déposés. Un travail
	// déjà remis n'est jamais réécrit — c'est l'invariant de tout le reste de
	// l'outil, et une marque invisible n'est pas une raison d'y déroger.
	ecrit := result.Status == Created ||
		(e.starter != nil && result.Starter != StarterNone &&
			result.Starter != StarterSkipped && result.Starter != StarterFailed)
	if options.Sign && !options.DryRun && ecrit {
		e.sign(item, &result, branch)
	}

	if !e.settings.AddCollaborator {
		return result
	}
	if options.DryRun {
		result.Collaborator = CollaboratorYes
		return result
	}
	// L'équipe reçoit le dépôt, pas ses membres : c'est ce qui fait qu'un
	// changement de composition suffit ensuite à changer qui y accède.
	if item.ForTeam() {
		if err := e.client.GrantTeamRepo(org, item.TeamSlug, org, item.Name,
			e.settings.Permission); err != nil {
			previous := result.Status
			result.Status = Failed
			result.Collaborator = "échec"
			result.Error = fmt.Sprintf("dépôt %s mais partage avec l'équipe impossible : %v",
				previous, err)
			return result
		}
		result.Collaborator = TeamShared
		return result
	}
	// Une personne qui travaille sous deux comptes n'a qu'un dépôt : elle y est
	// invitée sous chacun d'eux, faute de quoi la moitié de son travail se
	// ferait depuis un compte sans accès.
	comptes := comptesDe(item)
	if len(comptes) == 0 {
		result.Collaborator = CollaboratorUnknown
		return result
	}
	etats := make([]string, 0, 2)
	for _, compte := range comptes {
		state, err := e.client.AddCollaborator(org, item.Name, compte, e.settings.Permission)
		if err != nil {
			previous := result.Status
			result.Status = Failed
			result.Collaborator = "échec"
			result.Error = fmt.Sprintf("dépôt %s mais invitation impossible : %v", previous, err)
			return result
		}
		etats = append(etats, state)
	}
	result.Collaborator = strings.Join(etats, ", ")
	return result
}

// comptesDe rend les comptes à inviter sur le dépôt d'une personne. Un plan qui
// n'en nomme aucun retombe sur celui de la personne : c'est ce que fait un
// appelant qui ne connaît pas encore les autres.
func comptesDe(item plan.PlannedRepo) []string {
	propres := make([]string, 0, len(item.Accounts))
	vus := map[string]bool{}
	for _, compte := range item.Accounts {
		compte = strings.TrimSpace(compte)
		if compte == "" || vus[strings.ToLower(compte)] {
			continue
		}
		vus[strings.ToLower(compte)] = true
		propres = append(propres, compte)
	}
	if len(propres) == 0 && strings.TrimSpace(item.Person.Username) != "" {
		propres = append(propres, item.Person.Username)
	}
	return propres
}

// previewStarter annonce en simulation ce qui arriverait aux fichiers de départ.
func (e *Executor) previewStarter(item plan.PlannedRepo, branch string, force bool) string {
	planned := valid.Plural(len(e.starter.Files), "fichier prévu", "fichiers prévus")
	if force {
		return planned
	}
	head, err := e.client.BranchHead(e.settings.Org, item.Name, branch)
	if err != nil {
		return planned
	}
	if head != "" {
		return StarterSkipped
	}
	return planned
}

// applyStarter dépose les fichiers de départ ; renvoie faux si l'étape a échoué.
func (e *Executor) applyStarter(item plan.PlannedRepo, result *Result, branch string, force bool) bool {
	org := e.settings.Org
	head, err := e.client.BranchHead(org, item.Name, branch)
	if err != nil {
		result.Status, result.Starter = Failed, StarterFailed
		result.Error = "état du dépôt illisible : " + err.Error()
		return false
	}

	// Un dépôt déjà garni n'est jamais réécrit sans demande explicite : le
	// travail déjà remis par la personne doit être préservé.
	if result.Status == Existing && head != "" && !force {
		result.Starter = StarterSkipped
		return true
	}

	files := make([]ghapi.PushFile, 0, len(e.starter.Files))
	for _, file := range e.starter.Files {
		files = append(files, ghapi.PushFile{Path: file.Path, Mode: file.Mode, Content: file.Content})
	}
	count, err := e.client.PushFiles(org, item.Name, files, e.settings.CommitMessage, branch)
	if err != nil {
		result.Status, result.Starter = Failed, StarterFailed
		result.Error = "fichiers de départ non déposés : " + err.Error()
		return false
	}
	result.Starter = valid.Plural(count, "fichier")
	return true
}

func (e *Executor) create(item plan.PlannedRepo, templateOwner, templateRepo string) (*ghapi.Repo, error) {
	if templateOwner != "" {
		return e.client.GenerateFromTemplate(templateOwner, templateRepo, e.settings.Org,
			item.Name, e.settings.Private(), item.Description, e.settings.IncludeAllBranches)
	}
	// auto_init reste faux quand des fichiers de départ sont prévus : le dépôt
	// demeure vide, ce qui rend la reprise après échec possible.
	return e.client.CreateOrgRepo(e.settings.Org, item.Name, e.settings.Private(),
		item.Description, e.starter == nil)
}

// sign dépose la marque invisible dans le README du dépôt.
//
// Elle est écrite dans le fichier qui existe, jamais dans un fichier créé pour
// l'occasion : déposer un README que personne n'a demandé serait une
// modification visible du travail, pour une marque qui se veut invisible. Un
// dépôt sans README n'est donc pas signé, et le bilan le dit.
//
// Un échec n'arrête rien. Le dépôt est créé, l'étudiant peut travailler ; ce
// qui manque est un signal de plus dans une détection qui n'en dépend pas.
func (e *Executor) sign(item plan.PlannedRepo, result *Result, branch string) {
	org := e.settings.Org
	fichier, err := e.client.ReadFile(org, item.Name, signature.ReadmeFile, branch)
	if err != nil || fichier == nil {
		result.Signature = ""
		return
	}
	token, err := signature.New()
	if err != nil {
		return
	}
	// L'API Git plutôt que celle des contenus : la seconde exige le « sha » du
	// fichier qu'elle remplace, et le README existe forcément ici. La première
	// superpose sur l'arbre du parent, ce que fait déjà le dépôt des fichiers
	// de départ.
	signe := signature.Sign(fichier.Content, token)
	if _, err := e.client.PushFiles(org, item.Name, []ghapi.PushFile{{
		Path: signature.ReadmeFile, Mode: "100644", Content: signe,
	}}, SignMessage, branch); err != nil {
		return
	}
	result.Signature = signature.Text(token)
}

// SignMessage est le message du commit qui dépose la marque. Il ne dit pas ce
// qu'il fait : un message « ajoute une signature invisible » la rendrait
// parfaitement visible dans l'historique.
const SignMessage = "Prépare le dépôt"
