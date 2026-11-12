package web

import (
	"net/http"
	"path/filepath"
	"strings"
	"sync"

	"github.com/PierreOlivierBrillant/gh-milou/internal/plagiarism"
	"github.com/PierreOlivierBrillant/gh-milou/internal/similarity"
	"github.com/PierreOlivierBrillant/gh-milou/internal/valid"
)

// Un rapport est un fichier ; l'écran n'en est qu'une lecture.
//
// Cela paraît un détail et ne l'est pas. Un rapport qu'on rouvre ne relance
// rien : déplacer le seuil, trier les paires autrement, revenir dessus le
// lendemain ne coûte aucune requête. Et le jour où l'analyse tournera dans une
// GitHub Action, elle produira exactement ce fichier-là : l'écran n'aura pas à
// changer d'une ligne.

// reports garde les derniers rapports lus, pour ne pas relire un fichier de
// plusieurs méga-octets à chaque clic dans la page.
type reportCache struct {
	mutex   sync.Mutex
	loaded  map[string]*plagiarism.Report
	pairKey string
	pair    *plagiarism.Pair
}

// rememberReport retient un rapport qu'on vient de produire.
func (s *Server) rememberReport(report *plagiarism.Report) {
	s.reports.mutex.Lock()
	defer s.reports.mutex.Unlock()
	if s.reports.loaded == nil {
		s.reports.loaded = map[string]*plagiarism.Report{}
	}
	s.reports.loaded[report.Basename()] = report
}

// reportNamed rend un rapport par son nom, en le relisant au besoin.
func (s *Server) reportNamed(name string) (*plagiarism.Report, error) {
	name = strings.TrimSpace(name)
	// Le nom vient de l'adresse : il ne doit désigner qu'un fichier du dossier
	// des rapports, jamais un chemin que quelqu'un aurait composé.
	if name == "" || strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") {
		return nil, valid.Errorf("Rapport « %s » : nom invalide.", name)
	}

	s.reports.mutex.Lock()
	cached, known := s.reports.loaded[name]
	s.reports.mutex.Unlock()
	if known {
		return cached, nil
	}

	report, err := plagiarism.Load(
		filepath.Join(s.reportDir(), plagiarism.Dir, name+".json"))
	if err != nil {
		return nil, err
	}
	s.rememberReport(report)
	return report, nil
}

// handlePlagiarismReports énumère les rapports déjà produits sur ce poste.
func (s *Server) handlePlagiarismReports(writer http.ResponseWriter, _ *http.Request) {
	chemins := plagiarism.List(s.reportDir())
	trouves := make([]map[string]any, 0, len(chemins))
	for _, chemin := range chemins {
		report, err := plagiarism.Load(chemin)
		if err != nil {
			// Un rapport illisible ne doit pas rendre la liste illisible : il
			// y paraît avec son motif, et les autres restent ouvrables.
			trouves = append(trouves, map[string]any{
				"name":  strings.TrimSuffix(filepath.Base(chemin), ".json"),
				"error": err.Error(),
			})
			continue
		}
		trouves = append(trouves, map[string]any{
			"name": report.Basename(), "assignment": report.Request.Assignment,
			"created_at": report.CreatedAt, "works": report.Analyzed(),
			"matches": len(report.Result.Matches), "threshold": report.Result.Threshold,
			"problems": len(report.Problems),
		})
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"reports": trouves, "disclaimer": plagiarism.Disclaimer,
	})
}

// handlePlagiarismReport rend un rapport entier, sauf son index.
//
// L'index pèse le plus lourd — des centaines de milliers d'empreintes — et la
// page n'en fait rien : elle montre des paires, des motifs et un histogramme.
// Il reste dans le fichier, où il sert à rouvrir une paire et, plus tard, à
// republier l'index sans tout recalculer.
func (s *Server) handlePlagiarismReport(writer http.ResponseWriter, request *http.Request) {
	report, err := s.reportNamed(request.PathValue("name"))
	if err != nil {
		fail(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"name":       report.Basename(),
		"assignment": report.Request.Assignment,
		"org":        report.Request.Org,
		"created_at": report.CreatedAt,
		"disclaimer": report.Disclaimer,
		"profile":    report.Profile,
		"inspection": report.Request.Inspection,
		"works":      works(report),
		"result":     report.Result,
		"marks":      s.porteursDeMarques(report),
		"problems":   report.Problems,
		"inspected":  report.Inspected,
	})
}

// works rend les copies analysées, sans leurs empreintes : de quoi dessiner un
// nuage de points et une arborescence, pas de quoi recalculer quoi que ce soit.
func works(report *plagiarism.Report) []map[string]any {
	depistees := report.Screening()
	liste := make([]map[string]any, 0, len(report.Index.Works))
	for _, work := range report.Index.Works {
		chemins := make([]string, 0, len(work.Files))
		for _, file := range work.Files {
			chemins = append(chemins, file.Path)
		}
		liste = append(liste, map[string]any{
			"id": work.ID, "label": work.Name(), "origin": work.Origin,
			"files": chemins, "tokens": work.TokenCount(), "prints": work.PrintCount(),
			// Le travail dont l'index a fourni cette copie, quand elle vient
			// d'un index : la page en tire à qui la demander, et qu'elle ne
			// s'ouvrira pas.
			"screened": depistees[work.ID],
		})
	}
	return liste
}

// pairInput désigne une paire, et éventuellement deux fichiers dedans.
type pairInput struct {
	Left      string `json:"left"`
	Right     string `json:"right"`
	LeftPath  string `json:"left_path"`
	RightPath string `json:"right_path"`
}

// openPair charge une paire, en gardant la dernière ouverte.
//
// Ouvrir une paire retélécharge deux dépôts. Passer de la vue de projet à celle
// d'un fichier, puis d'un fichier à l'autre, ne doit pas les retélécharger à
// chaque fois : c'est le même couple, au même commit, et il ne bougera pas.
func (s *Server) openPair(report *plagiarism.Report, left, right string) (
	*plagiarism.Pair, error) {

	key := report.Basename() + "\x00" + left + "\x00" + right
	s.reports.mutex.Lock()
	if s.reports.pairKey == key && s.reports.pair != nil {
		pair := s.reports.pair
		s.reports.mutex.Unlock()
		return pair, nil
	}
	s.reports.mutex.Unlock()

	pair, err := plagiarism.OpenPair(s.deps.Client, report, left, right)
	if err != nil {
		return nil, err
	}
	s.reports.mutex.Lock()
	s.reports.pairKey, s.reports.pair = key, pair
	s.reports.mutex.Unlock()
	return pair, nil
}

// handlePlagiarismPair rend la vue « explorer deux projets ».
func (s *Server) handlePlagiarismPair(writer http.ResponseWriter, request *http.Request) {
	report, body, err := s.pairRequest(request)
	if err != nil {
		fail(writer, err)
		return
	}
	pair, err := s.openPair(report, body.Left, body.Right)
	if err != nil {
		fail(writer, err)
		return
	}
	// Qui a remis en premier, quand les deux dates sont connues. La phrase
	// vient du domaine : elle dit l'ordre et, dans le même souffle, ce qu'il ne
	// prouve pas.
	rendu := map[string]any{
		"match":      pair.Match,
		"project":    pair.Project(report),
		"disclaimer": report.Disclaimer,
	}
	if chrono, datee := report.Chronology(body.Left, body.Right); datee {
		rendu["chronology"] = chrono
	}
	writeJSON(writer, http.StatusOK, rendu)
}

// handlePlagiarismFile rend la vue « comparer deux fichiers ».
func (s *Server) handlePlagiarismFile(writer http.ResponseWriter, request *http.Request) {
	report, body, err := s.pairRequest(request)
	if err != nil {
		fail(writer, err)
		return
	}
	pair, err := s.openPair(report, body.Left, body.Right)
	if err != nil {
		fail(writer, err)
		return
	}
	view, err := pair.File(body.LeftPath, body.RightPath)
	if err != nil {
		fail(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, view)
}

func (s *Server) pairRequest(request *http.Request) (
	*plagiarism.Report, pairInput, error) {

	var body pairInput
	report, err := s.reportNamed(request.PathValue("name"))
	if err != nil {
		return nil, body, err
	}
	if err := decode(request, &body); err != nil {
		return nil, body, err
	}
	if body.Left == "" || body.Right == "" {
		return nil, body, valid.Errorf("Paire incomplète : deux copies sont attendues.")
	}
	return report, body, nil
}

// porteursDeMarques rend, pour chaque marque relevée en double, la personne à
// qui elle a été délivrée.
//
// La marque seule ne dit rien : c'est le registre qui répond à « de qui
// est-elle ». Un collègue qui ne l'a pas voit la coïncidence sans voir le nom,
// et c'est exactement ce qu'on veut — la coïncidence se constate à deux, la
// levée du nom appartient à qui a distribué le travail.
func (s *Server) porteursDeMarques(report *plagiarism.Report) map[string]any {
	porteurs := map[string]any{}
	org := strings.TrimSpace(report.Request.Org)
	if org == "" {
		org = s.org()
	}
	set, _ := s.names(org)
	for _, signal := range report.Result.Signals {
		if signal.Kind != similarity.SharedSignature || signal.Detail == "" {
			continue
		}
		titulaire, connu := set.Holder(signal.Detail)
		if !connu {
			continue
		}
		porteurs[signal.Detail] = map[string]any{
			"username": titulaire.Username, "full_name": titulaire.FullName,
			"student_id": titulaire.StudentID, "phrase": titulaire.Phrase(),
			"assignment": titulaire.Issued.Assignment, "issued_at": titulaire.Issued.IssuedAt,
		}
	}
	return porteurs
}
