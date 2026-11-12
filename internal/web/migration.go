package web

import (
	"net/http"
	"strings"

	"github.com/PierreOlivierBrillant/gh-milou/internal/classroom"
	"github.com/PierreOlivierBrillant/gh-milou/internal/groups"
	"github.com/PierreOlivierBrillant/gh-milou/internal/naming"
	"github.com/PierreOlivierBrillant/gh-milou/internal/valid"
)

// Déplacer un groupe, c'est renommer ses dépôts pour qu'ils tiennent à une
// autre place : une autre session, un autre cours, un autre numéro de groupe.
//
// GitHub garde une redirection depuis l'ancien nom : les clones et les liens
// déjà distribués continuent de fonctionner.

// migrationRow est un dépôt à renommer, ou la raison qui l'en empêche.
type migrationRow struct {
	Repo     string `json:"repo"`
	Target   string `json:"target,omitempty"`
	Student  string `json:"student,omitempty"`
	Username string `json:"username,omitempty"`
	Problem  string `json:"problem,omitempty"`
}

// Ready dit si la ligne peut être renommée.
func (r migrationRow) Ready() bool { return r.Problem == "" && r.Target != "" }

// bascule dit si le groupe peut suivre ses dépôts à leur nouvelle place. Il ne
// le peut que si aucun ne reste en arrière : un dépôt bloqué comme un dépôt en
// échec garde son ancien nom, et le groupe qui aurait basculé cesserait de le
// voir. C'est ce que promet la case « laisser en place » ; l'aperçu l'annonce,
// la migration l'applique.
func bascule(bloques, echecs int) bool { return bloques == 0 && echecs == 0 }

// migrationInput est ce que l'interface envoie pour préparer ou lancer une
// migration.
type migrationInput struct {
	Session string `json:"session"`
	Course  string `json:"course"`
	Group   string `json:"group"`
	// SkipBlocked laisse en place les dépôts qui ne peuvent pas être renommés,
	// au lieu de refuser la migration entière.
	SkipBlocked bool `json:"skip_blocked"`
}

// migrationPlan compose le renommage de tous les dépôts d'un groupe hérité.
func (s *Server) migrationPlan(request *http.Request, body migrationInput) (
	classroom.Classroom, classroom.Classroom, []migrationRow, error) {
	var vide classroom.Classroom
	cours, err := s.place(request)
	if err != nil {
		return cours, vide, nil, err
	}
	cible := cours
	session, err := naming.Fragment(body.Session, "Session")
	if err != nil {
		return cours, vide, nil, err
	}
	course, err := naming.Fragment(body.Course, "Cours")
	if err != nil {
		return cours, vide, nil, err
	}
	group, err := naming.Fragment(body.Group, "Groupe")
	if err != nil {
		return cours, vide, nil, err
	}
	cible.Session, cible.Course, cible.Group = session, course, group
	if strings.EqualFold(cours.Scope(), cible.Scope()) {
		return cours, vide, nil, valid.Errorf(
			"« %s » est déjà à cette place.", cours.Label())
	}

	repos, _, err := s.repos(cours.Org, false)
	if err == nil {
		cours = s.enrichi(cours, repos)
	}
	if err != nil {
		return cours, vide, nil, err
	}
	existants := map[string]bool{}
	for _, repo := range repos {
		existants[strings.ToLower(repo.Name)] = true
	}

	var lignes []migrationRow
	vises := map[string]string{} // nom visé → dépôt qui le vise déjà
	// Une migration ne renomme que les dépôts d'étudiants : ceux d'une équipe
	// sont nommés d'après elle, et un groupe hérité n'en a aucune.
	for _, travail := range cours.Assignments(repos, nil) {
		nom, err := naming.Fragment(cours.ShortName(travail.ID), "Travail")
		if err != nil {
			nom = ""
		}
		for _, repo := range cours.Repos(travail.ID, repos) {
			ligne := migrationRow{Repo: repo.Name}
			student, inscrit := cours.StudentOf(repo.Name)
			switch {
			case nom == "":
				ligne.Problem = "nom de travail illisible"
			case !inscrit:
				ligne.Problem = "« " + repo.Suffix + " » ne correspond à aucun étudiant du groupe"
			default:
				ligne.Username = student.Username
				fragment, err := naming.Student(student.FullName)
				if err != nil {
					ligne.Problem = "nom complet manquant pour @" + student.Username
					break
				}
				ligne.Student = fragment
				ligne.Target = naming.Compose(session, course, group, nom, fragment)
			}

			if ligne.Target != "" {
				cle := strings.ToLower(ligne.Target)
				switch {
				case vises[cle] != "":
					ligne.Problem = "même nom visé que « " + vises[cle] + " »"
					ligne.Target = ""
				case existants[cle]:
					ligne.Problem = "« " + ligne.Target + " » existe déjà"
					ligne.Target = ""
				default:
					vises[cle] = repo.Name
				}
			}
			lignes = append(lignes, ligne)
		}
	}
	return cours, cible, lignes, nil
}

// handleMigrationPreview montre le renommage sans rien écrire.
func (s *Server) handleMigrationPreview(writer http.ResponseWriter, request *http.Request) {
	var body migrationInput
	if err := decode(request, &body); err != nil {
		fail(writer, err)
		return
	}
	_, cible, lignes, err := s.migrationPlan(request, body)
	if err != nil {
		fail(writer, err)
		return
	}
	prets, bloques := 0, 0
	for _, ligne := range lignes {
		if ligne.Ready() {
			prets++
		} else {
			bloques++
		}
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"scope":   cible.Scope(),
		"session": cible.Session, "course": cible.Course, "group": cible.Group,
		"rows": lignes, "ready": prets, "blocked": bloques,
		"switch": bascule(bloques, 0),
	})
}

// handleMigrationApply renomme les dépôts, puis fait passer le groupe à la
// nouvelle nomenclature.
func (s *Server) handleMigrationApply(writer http.ResponseWriter, request *http.Request) {
	var body migrationInput
	if err := decode(request, &body); err != nil {
		fail(writer, err)
		return
	}
	cours, cible, lignes, err := s.migrationPlan(request, body)
	if err != nil {
		fail(writer, err)
		return
	}

	var prets []migrationRow
	var bloques []migrationRow
	for _, ligne := range lignes {
		if ligne.Ready() {
			prets = append(prets, ligne)
		} else {
			bloques = append(bloques, ligne)
		}
	}
	if len(bloques) > 0 && !body.SkipBlocked {
		fail(writer, valid.Errorf(
			"%s", valid.Plural(len(bloques),
				"dépôt ne peut pas être renommé : corrigez la liste des étudiants, "+
					"ou acceptez de le laisser en place.",
				"dépôts ne peuvent pas être renommés : corrigez la liste des étudiants, "+
					"ou acceptez de les laisser en place.")))
		return
	}
	if len(prets) == 0 {
		fail(writer, valid.Errorf("Aucun dépôt à renommer."))
		return
	}

	label := "Déplacement de « " + cours.Label() + " » vers " + cible.Scope()
	job := s.jobs.Start("migration", label, func(job *Job) (any, error) {
		renommes, echecs := 0, 0
		var suivis []groups.Renamed
		for index, ligne := range prets {
			if job.Canceled() {
				break
			}
			apres, err := s.deps.Client.RenameRepo(cours.Org, ligne.Repo, ligne.Target)
			if err != nil {
				echecs++
				job.Line(ligne.Repo+" : échec — "+err.Error(),
					map[string]string{"status": "échec"})
			} else {
				renommes++
				suivis = append(suivis, groups.Renamed{Before: ligne.Repo, After: apres.Info()})
				job.Line(ligne.Repo+" → "+ligne.Target,
					map[string]string{"status": "mis à jour"})
			}
			job.Progress(index+1, len(prets), ligne.Repo)
		}
		for _, ligne := range bloques {
			job.Warn(ligne.Repo + " laissé en place : " + ligne.Problem)
		}
		s.renamed(cours.Org, suivis)

		suit := bascule(len(bloques), echecs) && !job.Canceled()
		if suit {
			// Ce qu'on retenait du groupe suit ses dépôts à leur nouvelle place.
			if _, err := s.classrooms.Move(cours.Org, cours.Scope(), cible); err != nil {
				return nil, err
			}
		}
		return map[string]any{
			"renamed": renommes, "failed": echecs, "skipped": len(bloques),
			"switched": suit, "scope": cible.Scope(),
		}, nil
	})
	writeJSON(writer, http.StatusAccepted, job.State())
}
