package web

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/PierreOlivierBrillant/gh-milou/internal/classroom"
	"github.com/PierreOlivierBrillant/gh-milou/internal/groups"
	"github.com/PierreOlivierBrillant/gh-milou/internal/roster"
	"github.com/PierreOlivierBrillant/gh-milou/internal/valid"
)

// Déplacer des travaux, et non des personnes. Une organisation adoptée sans
// convention range parfois sous un même préfixe — « travail-de » — les travaux
// de plusieurs groupes et de plusieurs sessions. Déplacer les étudiants un à un
// ne les sépare pas : c'est le travail qui appartient à un groupe, et c'est lui
// qu'on veut en sortir.
//
// Rien n'oblige à connaître les personnes pour cela : un dépôt dont l'étudiant
// reste inconnu garde le dernier niveau de son nom. Il arrive à la bonne place,
// le groupe le reconnaît, et le nom complet se corrige ensuite — le crayon
// « Renommer… » de la liste des étudiants.

// relocateInput est ce que l'interface envoie pour déplacer des travaux.
type relocateInput struct {
	// Assignments désigne les travaux par leur identifiant, avec le nom qu'ils
	// porteront à l'arrivée — vide, ils gardent le leur.
	Assignments []classroom.Relocation `json:"assignments"`
	// Target est la place d'un groupe qui existe déjà ; NewGroup, à sa place,
	// décrit un groupe à déclarer pour l'occasion.
	Target   string     `json:"target"`
	NewGroup *movePlace `json:"new_group"`
}

// relocatePlan compose le déplacement demandé, sans rien écrire.
type relocatePlan struct {
	depart   classroom.Classroom
	arrivee  classroom.Classroom
	neuf     bool
	lignes   []classroom.Move
	suivent  []roster.Person
	quittent []roster.Person
}

// relocation compose le plan et refuse tout ce qui l'empêcherait d'aller
// jusqu'au bout : une place d'arrivée qui ne sait pas nommer, un travail sans
// dépôt, un nom déjà pris.
func (s *Server) relocation(request *http.Request, body relocateInput) (relocatePlan, error) {
	var plan relocatePlan
	depart, err := s.place(request)
	if err != nil {
		return plan, err
	}
	repos, _, err := s.repos(depart.Org, false)
	if err == nil {
		depart = s.enrichi(depart, repos)
	}
	if err != nil {
		return plan, err
	}
	arrivee, neuf, err := s.moveTarget(body.Target, body.NewGroup, repos)
	if err != nil {
		return plan, err
	}
	if classroom.NormalizeScope(depart.Scope()) == classroom.NormalizeScope(arrivee.Scope()) {
		return plan, valid.Errorf("Le groupe d'arrivée est celui de départ.")
	}
	lignes, err := classroom.PlanMoveAssignments(depart, arrivee, body.Assignments, repos)
	if err != nil {
		return plan, err
	}
	if len(lignes) == 0 {
		return plan, valid.Errorf("Aucun dépôt à renommer : ils sont déjà à cette place.")
	}
	suivent, quittent := classroom.Followers(depart, lignes, repos)
	return relocatePlan{
		depart: depart, arrivee: arrivee, neuf: neuf, lignes: lignes,
		suivent: suivent, quittent: quittent,
	}, nil
}

// handleRelocatePreview montre le renommage sans rien écrire.
func (s *Server) handleRelocatePreview(writer http.ResponseWriter, request *http.Request) {
	var body relocateInput
	if err := decode(request, &body); err != nil {
		fail(writer, err)
		return
	}
	plan, err := s.relocation(request, body)
	if err != nil {
		fail(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"rows": plan.lignes, "ready": len(plan.lignes),
		"target": plan.arrivee.Label(), "target_scope": plan.arrivee.Scope(),
		"created":  plan.neuf,
		"students": comptes(plan.suivent), "leaving": comptes(plan.quittent),
	})
}

// handleRelocate renomme les dépôts des travaux choisis, puis fait suivre les
// fiches. Les listes ne bougent qu'une fois tous les dépôts renommés : à
// mi-chemin, elles décriraient un rangement qui n'a pas eu lieu.
func (s *Server) handleRelocate(writer http.ResponseWriter, request *http.Request) {
	var body relocateInput
	if err := decode(request, &body); err != nil {
		fail(writer, err)
		return
	}
	plan, err := s.relocation(request, body)
	if err != nil {
		fail(writer, err)
		return
	}

	label := "Déplacement de " + travauxEnMots(body.Assignments) +
		" vers « " + plan.arrivee.Label() + " »"
	job := s.jobs.Start("deplacement", label, func(job *Job) (any, error) {
		renommes, echecs := 0, 0
		var suivis []groups.Renamed
		for index, ligne := range plan.lignes {
			if job.Canceled() {
				break
			}
			apres, err := s.deps.Client.RenameRepo(
				plan.depart.Org, ligne.Repo, ligne.Target)
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
			job.Progress(index+1, len(plan.lignes), ligne.Repo)
		}
		s.renamed(plan.depart.Org, suivis)

		bilan := map[string]any{
			"renamed": renommes, "failed": echecs,
			"target": plan.arrivee.Label(), "target_scope": plan.arrivee.Scope(),
			"created": plan.neuf, "moved": 0,
		}
		if echecs > 0 || job.Canceled() {
			job.Warn("Les listes n'ont pas bougé : tous les dépôts n'ont pas suivi.")
			return bilan, nil
		}
		// Les dates cibles suivent leurs travaux : ce sont les mêmes travaux,
		// rangés ailleurs, et leur identifiant au registre vient de changer.
		if suivies := classroom.MoveDue(
			plan.depart, plan.arrivee, body.Assignments); len(suivies) > 0 {
			if _, err := s.registryOf(plan.depart.Org).Apply(echeances(suivies)); err != nil {
				job.Warn("Dates cibles non reportées : " + err.Error())
			}
		}
		if _, err := s.classrooms.Save(plan.arrivee.With(plan.suivent...)); err != nil {
			return nil, err
		}
		// Le groupe de départ n'est réécrit que s'il perd quelqu'un : sinon on
		// lui inventerait une liste retenue qu'il n'avait pas.
		if len(plan.quittent) > 0 {
			if _, err := s.classrooms.Save(
				plan.depart.Without(plan.quittent...)); err != nil {
				return nil, err
			}
		}
		bilan["moved"] = len(plan.suivent)
		return bilan, nil
	})
	writeJSON(writer, http.StatusAccepted, job.State())
}

// travauxEnMots nomme ce qu'un déplacement emporte, pour le titre du journal.
func travauxEnMots(travaux []classroom.Relocation) string {
	if len(travaux) == 1 {
		return "« " + strings.TrimSpace(travaux[0].ID) + " »"
	}
	return strconv.Itoa(len(travaux)) + " travaux"
}
