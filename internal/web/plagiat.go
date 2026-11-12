package web

import (
	"net/http"
	"strings"

	"github.com/PierreOlivierBrillant/gh-milou/internal/classroom"
	"github.com/PierreOlivierBrillant/gh-milou/internal/corpus"
	"github.com/PierreOlivierBrillant/gh-milou/internal/groups"
	"github.com/PierreOlivierBrillant/gh-milou/internal/inspect"
	"github.com/PierreOlivierBrillant/gh-milou/internal/naming"
	"github.com/PierreOlivierBrillant/gh-milou/internal/plagiarism"
	"github.com/PierreOlivierBrillant/gh-milou/internal/registry"
	"github.com/PierreOlivierBrillant/gh-milou/internal/roster"
	"github.com/PierreOlivierBrillant/gh-milou/internal/rules"
	"github.com/PierreOlivierBrillant/gh-milou/internal/tokens"
	"github.com/PierreOlivierBrillant/gh-milou/internal/valid"
)

// La détection de plagiat, vue du navigateur.
//
// Rien de ce qui suit ne décide quoi que ce soit : les règles vivent dans
// « plagiarism », « corpus », « inspect » et « similarity », et cet écran n'en
// est qu'une façade. C'est ce qui garantit que l'assistant du terminal et la
// ligne de commande diront la même chose — le seuil proposé, les motifs
// d'écartement, l'avertissement.
//
// Trois moments, et l'ordre compte. On montre d'abord ce que le profil retient
// et ce que l'analyse coûtera, parce qu'une analyse lancée à l'aveugle sur cinq
// ans de dépôts se termine mal. On lance ensuite, en travail de fond. On ouvre
// enfin une paire, et c'est seulement là que du code est retéléchargé — pour
// deux dépôts, pas pour trois cents.

// plagiatInput est le réglage d'une analyse, tel que la page l'envoie.
type plagiatInput struct {
	// Reach dit jusqu'où le corpus s'étend : ce groupe, tout le cours, ou
	// toutes les sessions — équivalences de sigles comprises.
	Reach     string   `json:"reach"`
	Profile   string   `json:"profile"`
	Languages []string `json:"languages"`
	Include   []string `json:"include"`
	Exclude   []string `json:"exclude"`
	Root      string   `json:"root"`
	Kgram     int      `json:"kgram"`
	Window    int      `json:"window"`
	Noise     float64  `json:"noise"`
	MinSim    float64  `json:"min_similarity"`
	// Baseline dit s'il faut écarter le gabarit distribué. C'est coché par
	// défaut : l'outil sait quel modèle il a déposé, et ne pas s'en servir
	// reviendrait à gonfler tous les scores de la même quantité.
	Baseline bool `json:"baseline"`
	// Names restreint l'analyse à certains dépôts ; vide les prend tous.
	Names []string `json:"names"`
	// Archives sont des ZIP de copies reçues d'un collègue, sur cette machine.
	Archives []string `json:"archives"`
	// Indexes nomme les travaux d'un collègue dont l'index publié entre dans
	// le corpus : des empreintes, jamais du code.
	Indexes []string `json:"indexes"`
}

func (p plagiatInput) settings() inspect.Settings {
	return inspect.Settings{
		Profile: p.Profile, Languages: p.Languages,
		Include: p.Include, Exclude: p.Exclude, Root: p.Root,
	}
}

// handlePlagiarismOptions rend ce qui se choisit : les profils d'inspection,
// les langages reconnus, et les valeurs par défaut.
//
// La page ne code aucune de ces listes : elles viennent du domaine, et une
// addition — un profil déclaré par l'organisation, un langage de plus — y
// paraît sans qu'on touche au navigateur.
func (s *Server) handlePlagiarismOptions(writer http.ResponseWriter, _ *http.Request) {
	declarees := s.rulesOf(s.org())
	profils := inspect.Catalog(declarees.Profiles)
	langages := make([]map[string]any, 0, len(tokens.All()))
	for _, langage := range tokens.All() {
		langages = append(langages, map[string]any{
			"id": langage.ID, "label": langage.Label,
			"extensions": langage.Extensions,
			"kgram":      langage.Kgram, "window": langage.Window,
		})
	}
	portees := make([]map[string]any, 0, len(corpus.Reaches))
	for _, portee := range corpus.Reaches {
		portees = append(portees, map[string]any{
			"id": string(portee), "label": corpus.ReachLabels[portee],
		})
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"disclaimer": plagiarism.Disclaimer,
		"profiles":   profils,
		"languages":  langages,
		"reaches":    portees,
		"rules":      declarees,
		"defaults": map[string]any{
			"profile":        inspect.AllProfile,
			"reach":          string(corpus.ReachGroup),
			"noise":          noiseDefault(declarees),
			"min_similarity": similarityFloor,
			"baseline":       true,
		},
		"excluded": map[string]any{
			"directories": inspect.Directories,
			"locks":       inspect.Locks,
			"generated":   inspect.Generated,
			"binary":      inspect.BinaryExtensions,
		},
	})
}

// Les valeurs par défaut viennent du domaine : les redire ici en ferait un
// second exemplaire libre de diverger.
const (
	similarityNoise = 0.30
	similarityFloor = 0.05
)

// noiseDefault rend la part que l'équipe préfère, ou celle de l'outil.
func noiseDefault(declared rules.Rules) float64 {
	if declared.Defaults.Noise > 0 {
		return declared.Defaults.Noise
	}
	return similarityNoise
}

// rulesOf rend ce que l'organisation déclare, surchargé par le fichier passé au
// lancement.
//
// Les deux se superposent plutôt que de s'exclure : le registre porte ce que
// l'équipe a décidé, et le fichier permet d'essayer autre chose sans le
// réécrire — le temps d'une analyse, ou pour une exécution automatisée qui
// porte les siennes.
func (s *Server) rulesOf(org string) rules.Rules {
	declarees := rules.Rules{}
	if set, _ := s.names(org); set != nil {
		declarees = set.Rules()
	}
	return declarees.Merge(s.deps.Rules)
}

// handlePlagiarismPreview montre ce que le profil retient et ce que l'analyse
// coûtera, sur un échantillon réellement téléchargé.
//
// Les deux vont ensemble et se font d'un coup : prévoir le coût demande de
// mesurer sur de vrais dépôts, et mesurer sur de vrais dépôts donne du même
// mouvement la liste des fichiers retenus. Les séparer coûterait deux fois le
// téléchargement pour la même chose.
func (s *Server) handlePlagiarismPreview(writer http.ResponseWriter, request *http.Request) {
	cours, id, repos, err := s.assignmentOf(request)
	if err != nil {
		fail(writer, err)
		return
	}
	var body plagiatInput
	if err := decode(request, &body); err != nil {
		fail(writer, err)
		return
	}
	declarees := s.rulesOf(cours.Org)
	cibles, err := s.plagiatTargets(cours, id, repos, body, declarees)
	if err != nil {
		fail(writer, err)
		return
	}
	inspector, err := inspect.New(body.settings(), declarees.Profiles)
	if err != nil {
		fail(writer, err)
		return
	}

	options := corpus.Options{
		Inspector: inspector, Kgram: body.Kgram, Window: body.Window,
		Jobs: s.deps.Jobs,
	}
	apercu := corpus.Sample(s.deps.Client, cibles, options, corpus.DefaultSample)
	apercu.Estimate = apercu.Estimate.Against(corpus.RunnerBudget)

	writeJSON(writer, http.StatusOK, map[string]any{
		"assignment": cours.ShortName(id),
		"repos":      len(cibles),
		"places":     corpus.Places(cibles),
		"profile":    inspector.Profile(),
		"samples":    apercu.Samples,
		"estimate":   apercu.Estimate,
		"baseline":   cours.Defaults.Template,
		"disclaimer": plagiarism.Disclaimer,
	})
}

// handlePlagiarismRun lance l'analyse en travail de fond.
func (s *Server) handlePlagiarismRun(writer http.ResponseWriter, request *http.Request) {
	cours, id, repos, err := s.assignmentOf(request)
	if err != nil {
		fail(writer, err)
		return
	}
	var body plagiatInput
	if err := decode(request, &body); err != nil {
		fail(writer, err)
		return
	}
	declarees := s.rulesOf(cours.Org)
	cibles, err := s.plagiatTargets(cours, id, repos, body, declarees)
	if err != nil {
		fail(writer, err)
		return
	}

	requete := plagiarism.Request{
		// L'identifiant complet, et non le nom court : c'est lui qui désigne le
		// travail sans ambiguïté. Deux groupes qui donnent chacun un « tp1 »
		// produiraient sinon deux rapports de même nom, dont l'un écraserait
		// l'autre dans la liste.
		Assignment: id, Org: cours.Org,
		Targets: cibles, Inspection: body.settings(), Rules: declarees,
		Archives: body.Archives, Indexes: body.Indexes,
		Kgram: body.Kgram, Window: body.Window,
		Noise: body.Noise, MinSimilarity: body.MinSim, Jobs: s.deps.Jobs,
	}
	if body.Baseline {
		requete.Baseline = gabaritOf(cours)
	}
	if err := requete.Validate(); err != nil {
		fail(writer, err)
		return
	}

	client, dossier := s.deps.Client, s.reportDir()
	anciens, index := s.priorReports(), s.indexes(cours.Org)
	job := s.jobs.Start("plagiat",
		valid.Plural(len(cibles), "copie")+" de « "+cours.ShortName(id)+" »",
		func(job *Job) (any, error) {
			rapport, err := plagiarism.RunFrom(plagiarism.Sources{
				Client: client, Indexes: index, Prior: anciens,
			}, requete, func(done, total int, nom string) {
				job.Progress(done, total, nom)
			})
			if err != nil {
				return nil, err
			}
			for _, souci := range rapport.Problems {
				job.Warn(souci.Repo + " : " + souci.Reason)
			}
			chemin, _, err := rapport.Save(dossier)
			if err != nil {
				return nil, err
			}
			s.rememberReport(rapport)
			// Le rapport est écrit avant le refus : même stérile, il dit ce qui
			// s'est passé dépôt par dépôt, et c'est ce qu'il faut regarder.
			if err := rapport.Barren(); err != nil {
				return nil, err
			}
			return plagiatSummary(rapport, chemin), nil
		})
	writeJSON(writer, http.StatusAccepted, job.State())
}

// plagiatSummary est ce que le travail de fond rend une fois fini : de quoi
// ouvrir le rapport, sans le rapport lui-même — il pèse parfois des méga-octets,
// et la page ira le chercher.
func plagiatSummary(rapport *plagiarism.Report, chemin string) map[string]any {
	return map[string]any{
		"report":     rapport.Basename(),
		"path":       chemin,
		"works":      rapport.Analyzed(),
		"matches":    len(rapport.Result.Matches),
		"threshold":  rapport.Result.Threshold,
		"problems":   len(rapport.Problems),
		"reused":     rapport.Reused,
		"received":   rapport.Received,
		"screened":   rapport.Screened,
		"signals":    len(rapport.Result.Signals),
		"disclaimer": plagiarism.Disclaimer,
	}
}

// gabaritOf rend le dépôt modèle du groupe, quand il y en a un.
//
// C'est l'avantage décisif sur un comparateur générique : celui-ci demande à
// l'enseignant de lui désigner le squelette commun, alors que l'outil sait
// lequel il a distribué. Un modèle mal formé n'arrête rien — l'analyse se fait
// sans lui, et le rapport le dit.
func gabaritOf(cours classroom.Classroom) []corpus.Target {
	modele := strings.TrimSpace(cours.Defaults.Template)
	owner, repo, coupe := strings.Cut(modele, "/")
	if !coupe || owner == "" || repo == "" {
		return nil
	}
	return []corpus.Target{{
		ID: modele, Label: "gabarit distribué", Owner: owner, Repo: repo,
	}}
}

// plagiatTargets dresse la liste des copies à comparer.
//
// La sélection elle-même — quels dépôts, quelles équivalences de sigles —
// appartient au domaine : cet écran ne fait qu'y ajouter ce que seul le
// navigateur sait, c'est-à-dire les noms complets et les dépôts que l'on vient
// de cocher.
func (s *Server) plagiatTargets(cours classroom.Classroom, id string,
	repos []groups.RepoInfo, body plagiatInput, declared rules.Rules) ([]corpus.Target, error) {

	reach, err := corpus.ParseReach(body.Reach)
	if err != nil {
		return nil, err
	}
	cibles, err := corpus.Select(cours.Org, repos, id, reach, declared)
	if err != nil {
		return nil, err
	}

	// Cocher des dépôts n'a de sens que sur le groupe qu'on regarde : une
	// sélection faite ici ne peut pas désigner les copies d'une autre session.
	retenus := map[string]bool{}
	for _, nom := range body.Names {
		retenus[strings.ToLower(strings.TrimSpace(nom))] = true
	}

	envois := make(map[string]string, len(repos))
	for _, repo := range repos {
		envois[strings.ToLower(repo.Name)] = repo.PushedAt
	}
	set, _ := s.names(cours.Org)

	// Les remises déjà relevées sont versées au passage : elles ne coûtent
	// aucune requête et disent qui a remis en premier.
	noms := make([]string, 0, len(cibles))
	for _, cible := range cibles {
		noms = append(noms, cible.Repo)
	}
	remises := s.remisesConnues(cours.Org, noms, repos)

	gardees := make([]corpus.Target, 0, len(cibles))
	for _, cible := range cibles {
		if len(retenus) > 0 && cible.Origin == cours.Scope() &&
			!retenus[strings.ToLower(cible.Repo)] {
			continue
		}
		cible.PushedAt = envois[strings.ToLower(cible.Repo)]
		cible.Label = s.nomDeCopie(cours, set, cible.Repo)
		cible.HandedIn = cours.HandedIn(remises[cible.Repo])
		// L'identité ne sert pas à l'analyse : elle sert à l'effacer, le jour
		// où l'on anonymise ces copies ou qu'on en publie l'index. La remplir
		// ici plutôt qu'au moment de publier évite d'en avoir deux versions.
		cible.Person = identiteDe(cours, set, cible.Repo)
		gardees = append(gardees, cible)
	}
	if len(gardees) < 2 {
		return nil, valid.Errorf(
			"Détection de plagiat : il faut au moins deux copies à comparer (%d retenue).",
			len(gardees))
	}
	return gardees, nil
}

// nomDeCopie retrouve le nom complet derrière un dépôt.
//
// Le groupe ouvert le sait pour les siens ; pour les copies d'une autre session
// ou d'un autre groupe, c'est le registre de l'organisation qui répond — c'est
// exactement ce qu'il existe pour faire.
// identiteDe retrouve la personne derrière un dépôt : le groupe ouvert pour les
// siens, le registre de l'organisation pour les autres sessions.
func identiteDe(cours classroom.Classroom, set *registry.Set, repo string) roster.Person {
	if student, inscrit := cours.StudentOf(repo); inscrit {
		return student
	}
	if user, connu := set.Find(repo); connu {
		return user.Person()
	}
	if parts, reconnu := naming.Parse(repo); reconnu && set != nil {
		if user, connu := set.Resolve(parts.Student); connu {
			return user.Person()
		}
	}
	return roster.Person{}
}

func (s *Server) nomDeCopie(cours classroom.Classroom, set *registry.Set, repo string) string {
	if student, inscrit := cours.StudentOf(repo); inscrit && student.FullName != "" {
		return student.FullName
	}
	return set.NameFor(repo)
}

// priorReports rend les derniers rapports de ce poste, pour reprendre ce qu'ils
// ont déjà empreinté plutôt que de le retélécharger.
func (s *Server) priorReports() []*plagiarism.Report {
	chemins := plagiarism.List(s.reportDir())
	if len(chemins) > MaxPriorReports {
		chemins = chemins[:MaxPriorReports]
	}
	anciens := make([]*plagiarism.Report, 0, len(chemins))
	for _, chemin := range chemins {
		// Un rapport illisible n'arrête rien : il n'apporte simplement pas de
		// reprise, et l'analyse retéléchargera ce qu'il aurait épargné.
		if rapport, err := plagiarism.Load(chemin); err == nil {
			anciens = append(anciens, rapport)
		}
	}
	return anciens
}

// MaxPriorReports borne ce qu'on relit pour chercher des empreintes
// réutilisables. Au-delà, on relirait des méga-octets de rapports pour épargner
// quelques téléchargements.
const MaxPriorReports = 8
