package web

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/PierreOlivierBrillant/gh-milou/internal/classroom"
	"github.com/PierreOlivierBrillant/gh-milou/internal/groups"
	"github.com/PierreOlivierBrillant/gh-milou/internal/naming"
	"github.com/PierreOlivierBrillant/gh-milou/internal/roster"
	"github.com/PierreOlivierBrillant/gh-milou/internal/valid"
)

// Déplacer des personnes d'un groupe à l'autre. Leurs dépôts suivent toujours :
// c'est leur nom qui dit à quel groupe ils appartiennent, et une fiche qui
// prétendrait le contraire ne serait vraie que sur cette machine.

// movePlace décrit un groupe d'arrivée qui n'existe pas encore : le
// déplacement le déclare au passage, plutôt que d'obliger à le créer d'abord
// puis à revenir.
type movePlace struct {
	Session string `json:"session"`
	Course  string `json:"course"`
	Group   string `json:"group"`
}

// moveInput est ce que l'interface envoie pour déplacer des étudiants.
type moveInput struct {
	// Username désigne une personne seule ; Usernames, plusieurs. Les deux se
	// cumulent : l'interface envoie l'un ou l'autre selon ce qui est coché.
	// Chacun est un compte ou, pour qui n'en a pas encore, un matricule.
	Username  string   `json:"username"`
	Usernames []string `json:"usernames"`
	// Target est la place d'un groupe qui existe déjà.
	Target string `json:"target"`
	// NewGroup, à sa place, décrit un groupe à déclarer pour l'occasion.
	NewGroup *movePlace `json:"new_group"`
}

// handleMoveStudent déplace une ou plusieurs personnes d'un groupe vers un
// autre — existant, ou déclaré pour l'occasion.
//
// Les dépôts sont renommés d'abord, les listes n'écoutent qu'ensuite. C'est
// GitHub qui dit à quel groupe un dépôt appartient : une liste qui aurait bougé
// sans lui décrirait un rangement qui n'a pas eu lieu, et le groupe de départ
// continuerait de montrer la personne que ses dépôts lui rattachent encore.
func (s *Server) handleMoveStudent(writer http.ResponseWriter, request *http.Request) {
	var body moveInput
	if err := decode(request, &body); err != nil {
		fail(writer, err)
		return
	}

	depart, err := s.place(request)
	if err != nil {
		fail(writer, err)
		return
	}
	repos, _, err := s.repos(depart.Org, false)
	if err == nil {
		depart = s.enrichi(depart, repos)
	}
	if err != nil {
		fail(writer, err)
		return
	}
	arrivee, neuf, err := s.moveTarget(body.Target, body.NewGroup, repos)
	if err != nil {
		fail(writer, err)
		return
	}
	if classroom.NormalizeScope(depart.Scope()) == classroom.NormalizeScope(arrivee.Scope()) {
		fail(writer, valid.Errorf("Le groupe d'arrivée est celui de départ."))
		return
	}

	// Ce que le groupe de départ ne connaît pas refuse le tout, d'emblée.
	personnes, err := depart.Movers(append(append([]string(nil), body.Usernames...), body.Username))
	if err != nil {
		fail(writer, err)
		return
	}

	// Le plan se compose entièrement avant le premier renommage : une collision
	// refuse le déplacement au lieu de l'interrompre à mi-chemin.
	renommages, err := classroom.PlanMove(depart, arrivee, personnes, repos)
	if err != nil {
		fail(writer, err)
		return
	}

	bilan := map[string]any{
		"moved": []string{}, "count": 0,
		"target": arrivee.Label(), "target_scope": arrivee.Scope(),
		"created": neuf, "renamed": 0, "failed": 0,
	}

	// Une personne sans dépôt n'existe que dans la liste : il n'y a rien à
	// renommer, et rien qui puisse contredire ce qu'on y écrit.
	if len(renommages) == 0 {
		if err := s.suivent(depart, arrivee, personnes); err != nil {
			fail(writer, err)
			return
		}
		bilan["moved"], bilan["count"] = designees(personnes), len(personnes)
		writeJSON(writer, http.StatusOK, bilan)
		return
	}

	label := "Déplacement de " + personnesEnMots(personnes) +
		" vers « " + arrivee.Label() + " »"
	job := s.jobs.Start("deplacement", label, func(job *Job) (any, error) {
		renommes, echecs := 0, 0
		var suivis []groups.Renamed
		for index, ligne := range renommages {
			if job.Canceled() {
				break
			}
			apres, err := s.deps.Client.RenameRepo(depart.Org, ligne.Repo, ligne.Target)
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
			job.Progress(index+1, len(renommages), ligne.Repo)
		}
		s.renamed(depart.Org, suivis)
		bilan["renamed"], bilan["failed"] = renommes, echecs

		if echecs > 0 || job.Canceled() {
			job.Warn("Les listes n'ont pas bougé : tous les dépôts n'ont pas suivi.")
			return bilan, nil
		}
		if err := s.suivent(depart, arrivee, personnes); err != nil {
			return nil, err
		}
		bilan["moved"], bilan["count"] = designees(personnes), len(personnes)
		return bilan, nil
	})
	writeJSON(writer, http.StatusAccepted, job.State())
}

// suivent fait suivre les fiches une fois les dépôts arrivés : elles quittent
// la liste du départ et rejoignent celle de l'arrivée.
func (s *Server) suivent(depart, arrivee classroom.Classroom, personnes []roster.Person) error {
	if _, err := s.classrooms.Save(depart.Without(personnes...)); err != nil {
		return err
	}
	_, err := s.classrooms.Save(arrivee.With(personnes...))
	return err
}

// moveTarget résout le groupe d'arrivée : une place déjà occupée, ou une place
// libre que le déplacement déclare. Déclarer sur une place occupée serait une
// fusion déguisée : elle est refusée, la place se choisit alors dans la liste.
func (s *Server) moveTarget(cible string, neuf *movePlace, repos []groups.RepoInfo) (
	classroom.Classroom, bool, error) {
	if neuf == nil {
		arrivee, err := s.placeAt(cible)
		return arrivee, false, err
	}
	session, err := naming.Fragment(neuf.Session, "Session")
	if err != nil {
		return classroom.Classroom{}, false, err
	}
	course, err := naming.Fragment(neuf.Course, "Cours")
	if err != nil {
		return classroom.Classroom{}, false, err
	}
	group, err := naming.Fragment(neuf.Group, "Groupe")
	if err != nil {
		return classroom.Classroom{}, false, err
	}
	place := naming.Prefix(session, course, group)
	for _, connu := range s.visibles(s.org(), repos) {
		if classroom.NormalizeScope(connu.Scope()) == classroom.NormalizeScope(place) {
			return classroom.Classroom{}, false, valid.Errorf(
				"« %s » existe déjà : choisissez-le dans la liste des groupes.", place)
		}
	}
	arrivee, err := classroom.AtScope(s.org(), place, classroom.DefaultsFrom(s.Settings()))
	return arrivee, true, err
}

// comptes rend les comptes GitHub d'une liste de personnes, tous ceux d'une
// même personne compris : c'est ce que nomme le bilan d'une équipe déplacée,
// dont chaque membre a un compte.
func comptes(personnes []roster.Person) []string {
	liste := make([]string, 0, len(personnes))
	for _, personne := range personnes {
		liste = append(liste, personne.Accounts()...)
	}
	return liste
}

// designees rend ce qui désigne chaque personne déplacée, comme le navigateur
// l'a envoyé : son compte, à défaut son matricule.
func designees(personnes []roster.Person) []string {
	liste := make([]string, 0, len(personnes))
	for _, personne := range personnes {
		liste = append(liste, classroom.Handle(personne))
	}
	return liste
}

// personnesEnMots nomme ce qu'un déplacement emporte, pour le titre du journal.
// Une personne seule s'y dit comme au terminal : par son compte, à défaut par
// son nom, à défaut par son matricule. Qui n'a pas encore de compte n'y
// laissait qu'une arobase.
func personnesEnMots(personnes []roster.Person) string {
	if len(personnes) != 1 {
		return strconv.Itoa(len(personnes)) + " étudiants"
	}
	personne := personnes[0]
	if compte := strings.TrimSpace(personne.Username); compte != "" {
		return "@" + compte
	}
	if nom := strings.TrimSpace(personne.FullName); nom != "" {
		return nom
	}
	return "matricule " + strings.TrimSpace(personne.StudentID)
}
