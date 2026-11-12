package web

import (
	"net/http"
	"strings"

	"github.com/PierreOlivierBrillant/gh-milou/internal/config"
	"github.com/PierreOlivierBrillant/gh-milou/internal/naming"
	"github.com/PierreOlivierBrillant/gh-milou/internal/plan"
	"github.com/PierreOlivierBrillant/gh-milou/internal/roster"
	"github.com/PierreOlivierBrillant/gh-milou/internal/starter"
	"github.com/PierreOlivierBrillant/gh-milou/internal/valid"
)

// ------------------------------------------------------------ liste des personnes

// rosterPayload est le résultat d'une lecture de liste.
type rosterPayload struct {
	Path   string          `json:"path,omitempty"`
	People []roster.Person `json:"people"`
	Issues []roster.Issue  `json:"issues"`
}

// handleParseRoster lit une liste collée ou déposée dans le navigateur.
func (s *Server) handleParseRoster(writer http.ResponseWriter, request *http.Request) {
	var body struct {
		Text string `json:"text"`
		// Content porte les octets d'un fichier déposé dans la page : le
		// navigateur n'en donne jamais le chemin, mais il en donne le contenu.
		Content []byte `json:"content"`
	}
	if err := decode(request, &body); err != nil {
		fail(writer, err)
		return
	}
	list := roster.Parse(body.Text)
	if len(body.Content) > 0 {
		list = roster.ParseBytes(body.Content)
	}
	// Toutes les personnes lues, compte ou non : c'est la page qui montrera
	// lesquelles restent à rattacher, et elle ne le peut pas si on les lui tait.
	writeJSON(writer, http.StatusOK, rosterPayload{
		People: list.Everyone(), Issues: list.Issues})
}

// handleLoadRoster lit une liste depuis un fichier de la machine.
func (s *Server) handleLoadRoster(writer http.ResponseWriter, request *http.Request) {
	var body struct {
		Path string `json:"path"`
	}
	if err := decode(request, &body); err != nil {
		fail(writer, err)
		return
	}
	list, err := roster.Load(body.Path)
	if err != nil {
		fail(writer, err)
		return
	}
	path, err := roster.ExpandPath(body.Path)
	if err != nil {
		path = body.Path
	}
	writeJSON(writer, http.StatusOK, rosterPayload{
		Path: path, People: list.Everyone(), Issues: list.Issues,
	})
}

// handleSaveRoster écrit une liste saisie dans l'interface, au format CSV.
func (s *Server) handleSaveRoster(writer http.ResponseWriter, request *http.Request) {
	var body struct {
		Path   string          `json:"path"`
		People []roster.Person `json:"people"`
	}
	if err := decode(request, &body); err != nil {
		fail(writer, err)
		return
	}
	if len(body.People) == 0 {
		fail(writer, valid.Errorf("Liste vide : rien à enregistrer."))
		return
	}
	saved, err := roster.Write(body.Path, body.People)
	if err != nil {
		fail(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]string{"path": saved})
}

// ------------------------------------------------------------------ paramètres

// assignmentIdentifier valide un identifiant de travail déjà composé. Chaque
// niveau est vérifié séparément : slugifier l'ensemble écraserait le point qui
// les sépare, et « 5n6.a26-01.tp1 » redeviendrait « 5n6-a26-01-tp1 ».
func assignmentIdentifier(value string) (string, error) {
	cleaned := strings.TrimSpace(value)
	if cleaned == "" {
		return "", valid.Errorf("Travail : la valeur est vide.")
	}
	niveaux := strings.Split(cleaned, naming.Separator)
	rendus := make([]string, 0, len(niveaux))
	for _, niveau := range niveaux {
		fragment, err := valid.SlugFragment(niveau, "Travail")
		if err != nil {
			return "", err
		}
		rendus = append(rendus, fragment)
	}
	return strings.Join(rendus, naming.Separator), nil
}

// normalize valide les réglages reçus du navigateur avant toute écriture.
func normalize(settings config.Settings) (config.Settings, error) {
	org, err := valid.Login(settings.Org, "Organisation")
	if err != nil {
		return settings, err
	}
	settings.Org = org

	assignment, err := assignmentIdentifier(settings.Assignment)
	if err != nil {
		return settings, err
	}
	settings.Assignment = assignment

	pattern, err := plan.ValidatePattern(settings.NamePattern, "Gabarit de nom", true)
	if err != nil {
		return settings, err
	}
	settings.NamePattern = pattern

	if strings.TrimSpace(settings.DescriptionPattern) != "" {
		description, err := plan.ValidatePattern(settings.DescriptionPattern, "Gabarit de description", false)
		if err != nil {
			return settings, err
		}
		settings.DescriptionPattern = description
	}

	if strings.TrimSpace(settings.Template) != "" {
		owner, repo, err := valid.RepoRef(settings.Template)
		if err != nil {
			return settings, err
		}
		settings.Template = owner + "/" + repo
	} else {
		settings.Template = ""
	}

	visibility, err := config.ValidateVisibility(settings.Visibility)
	if err != nil {
		return settings, err
	}
	settings.Visibility = visibility

	permission, err := config.ValidatePermission(settings.Permission)
	if err != nil {
		return settings, err
	}
	settings.Permission = permission

	if settings.DelaySeconds < 0 {
		settings.DelaySeconds = 0
	}
	if strings.TrimSpace(settings.CommitMessage) == "" {
		settings.CommitMessage = config.DefaultCommitMessage
	}
	return settings, nil
}

// sanitize nettoie des réglages destinés au disque, sans exiger qu'ils soient
// complets : mémoriser un travail en cours de saisie doit rester possible.
func sanitize(settings config.Settings) (config.Settings, error) {
	if strings.TrimSpace(settings.Org) != "" {
		org, err := valid.Login(settings.Org, "Organisation")
		if err != nil {
			return settings, err
		}
		settings.Org = org
	}
	if strings.TrimSpace(settings.Visibility) != "" {
		visibility, err := config.ValidateVisibility(settings.Visibility)
		if err != nil {
			return settings, err
		}
		settings.Visibility = visibility
	}
	if strings.TrimSpace(settings.Permission) != "" {
		permission, err := config.ValidatePermission(settings.Permission)
		if err != nil {
			return settings, err
		}
		settings.Permission = permission
	}
	if settings.DelaySeconds < 0 {
		settings.DelaySeconds = 0
	}
	return settings, nil
}

// planItem est une ligne du plan, telle que l'affiche l'interface.
type planItem struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	FullName    string `json:"full_name"`
	Username    string `json:"username"`
}

// rows met le plan sous la forme attendue par l'interface.
func rows(items []plan.PlannedRepo) []planItem {
	list := make([]planItem, 0, len(items))
	for _, item := range items {
		list = append(list, planItem{
			Name: item.Name, Description: item.Description,
			FullName: item.Person.FullName, Username: item.Person.Username,
		})
	}
	return list
}

// handleCheckTemplate vérifie qu'un dépôt modèle existe et en est bien un.
func (s *Server) handleCheckTemplate(writer http.ResponseWriter, request *http.Request) {
	var body struct {
		Template string `json:"template"`
	}
	if err := decode(request, &body); err != nil {
		fail(writer, err)
		return
	}
	owner, repo, err := valid.RepoRef(body.Template)
	if err != nil {
		fail(writer, err)
		return
	}
	data, err := s.deps.Client.GetRepo(owner, repo)
	if err != nil {
		fail(writer, err)
		return
	}
	if data == nil {
		fail(writer, valid.Errorf("Dépôt modèle « %s/%s » introuvable pour @%s.",
			owner, repo, s.deps.Viewer))
		return
	}
	warning := ""
	if !data.IsTemplate {
		warning = "« " + owner + "/" + repo + " » n'est pas déclaré comme dépôt modèle " +
			"(réglage « Template repository » sur GitHub)."
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"template": owner + "/" + repo, "is_template": data.IsTemplate, "warning": warning,
	})
}

// starterFile décrit un fichier de départ dans l'interface.
type starterFile struct {
	Path  string `json:"path"`
	Size  int    `json:"size"`
	Label string `json:"label"`
}

// handleInspectStarter lit le dossier de fichiers de départ et le décrit.
func (s *Server) handleInspectStarter(writer http.ResponseWriter, request *http.Request) {
	var body struct {
		Path string `json:"path"`
	}
	if err := decode(request, &body); err != nil {
		fail(writer, err)
		return
	}
	bundle, err := starter.Load(body.Path)
	if err != nil {
		fail(writer, err)
		return
	}
	files := make([]starterFile, 0, len(bundle.Files))
	for _, file := range bundle.Files {
		files = append(files, starterFile{
			Path: file.Path, Size: file.Size(), Label: starter.HumanSize(file.Size()),
		})
	}
	warning := ""
	if bundle.NeedsWorkflowScope() {
		if present, known := s.deps.Client.HasScope("workflow"); known && !present {
			warning = "Des fichiers visent .github/workflows : la portée « workflow » est requise " +
				"(gh auth refresh -s workflow)."
		}
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"root": bundle.Root, "files": files, "skipped": bundle.Skipped,
		"summary": bundle.Describe(), "large": bundle.IsLarge(), "warning": warning,
	})
}

// ------------------------------------------------------- vérification des comptes

// handleVerifyAccounts confronte chaque compte GitHub à l'API.
func (s *Server) handleVerifyAccounts(writer http.ResponseWriter, request *http.Request) {
	var body struct {
		People []roster.Person `json:"people"`
	}
	if err := decode(request, &body); err != nil {
		fail(writer, err)
		return
	}
	if len(body.People) == 0 {
		fail(writer, valid.Errorf("Aucune personne à vérifier."))
		return
	}

	// Seuls les comptes connus se vérifient : une personne qu'on n'a pas encore
	// rattachée n'a pas de compte introuvable, elle n'en a pas du tout.
	people := roster.WithAccounts(body.People)
	job := s.jobs.Start("comptes", "Vérification de "+valid.Plural(len(people), "compte"),
		func(job *Job) (any, error) {
			missing := make([]roster.Person, 0)
			for index, person := range people {
				if job.Canceled() {
					return nil, nil
				}
				exists, err := s.deps.Client.UserExists(person.Username)
				if err != nil {
					return nil, err
				}
				if !exists {
					missing = append(missing, person)
					job.Warn("@" + person.Username + " : compte introuvable")
				}
				job.Progress(index+1, len(people), "@"+person.Username)
			}
			return map[string]any{"checked": len(people), "missing": missing}, nil
		})
	writeJSON(writer, http.StatusAccepted, job.State())
}
