package web_test

import (
	"net/http"
	"sort"
	"strings"
	"testing"

	"github.com/PierreOlivierBrillant/gh-milou/internal/fakegh"
)

// Une personne, plusieurs comptes.
//
// Un étudiant travaille parfois sous deux comptes. Rien ne le déduit de son
// nom — deux étudiants peuvent s'appeler pareil, et les confondre leur donnerait
// un dépôt pour deux : c'est une décision, prise et écrite.

// rattacher déclare qu'un compte est celui d'une personne déjà inscrite.
func (h *harnais) rattacher(place, personne, compte string) (*http.Response, []byte) {
	h.t.Helper()
	return h.requete(http.MethodPost, "/api/classrooms/"+place+"/students/accounts",
		map[string]any{"username": personne, "account": compte})
}

func TestDeuxComptesNeFontQuUnePersonne(t *testing.T) {
	state := fakegh.NewState()
	state.Users["ecote"] = "Émilie Côté"
	h := nouveau(t, state)
	place := h.groupe("a26", "5n6", "01",
		"Émilie Côté", "emilie-cote", "Jean-Luc Picard", "jlpicard")

	if reponse, contenu := h.rattacher(place, "emilie-cote", "ecote"); reponse.StatusCode >= 300 {
		t.Fatalf("rattachement refusé : %d — %s", reponse.StatusCode, contenu)
	}

	// Un seul dépôt : c'est son nom qui le nomme, pas son compte.
	h.travail(http.MethodPost, "/api/classrooms/"+place+"/assignments",
		map[string]any{"name": "tp1"})
	noms := h.depots()
	sort.Strings(noms)
	attendus := []string{"a26.5n6.01.tp1.emilie-cote", "a26.5n6.01.tp1.jean-luc-picard"}
	if strings.Join(noms, ",") != strings.Join(attendus, ",") {
		t.Fatalf("un dépôt par personne attendu : %v", noms)
	}

	// Mais elle y est invitée sous ses deux comptes.
	invites := map[string]bool{}
	for _, invitation := range h.State.Invitations["acme/a26.5n6.01.tp1.emilie-cote"] {
		invites[invitation.Login] = true
	}
	if !invites["emilie-cote"] || !invites["ecote"] {
		t.Fatalf("les deux comptes devraient être invités : %v", invites)
	}
}

// Le rattachement retire le compte de la liste s'il y était inscrit à part :
// la même personne n'y figure pas deux fois.
func TestRattacherRetireLInscriptionSeparee(t *testing.T) {
	state := fakegh.NewState()
	state.Users["ecote"] = ""
	h := nouveau(t, state)
	place := h.groupe("a26", "5n6", "01",
		"Émilie Côté", "emilie-cote", "", "ecote")

	h.rattacher(place, "emilie-cote", "ecote")

	var fiche struct {
		Students []struct {
			FullName string   `json:"full_name"`
			Username string   `json:"username"`
			Also     []string `json:"also"`
		} `json:"students"`
	}
	h.json(http.MethodGet, "/api/classrooms/"+place, nil, &fiche)
	if len(fiche.Students) != 1 {
		t.Fatalf("une seule personne attendue : %+v", fiche.Students)
	}
	if strings.Join(fiche.Students[0].Also, ",") != "ecote" {
		t.Fatalf("le second compte devrait être rattaché : %+v", fiche.Students[0])
	}
}

// Rien ne se déduit du nom : deux homonymes restent deux personnes, et la
// préparation les refuse — c'est ce qui les empêche de partager un dépôt.
func TestDeuxHomonymesRestentDeuxPersonnes(t *testing.T) {
	h := nouveau(t, nil)
	place := h.groupe("a26", "5n6", "01",
		"Jean Tremblay", "jtremblay", "Jean Tremblay", "jean-t")

	reponse, contenu := h.requete(http.MethodPost,
		"/api/classrooms/"+place+"/assignments/preview", map[string]any{"name": "tp1"})
	if reponse.StatusCode != http.StatusBadRequest {
		t.Fatalf("statut %d, attendu 400 — %s", reponse.StatusCode, contenu)
	}
	if !strings.Contains(string(contenu), "Jean Tremblay") {
		t.Fatalf("message peu explicite : %s", contenu)
	}
}

// Le dépôt arrivé sous l'un de ses comptes est le sien sous l'autre aussi.
func TestUnDepotSousLAutreCompteResteLeSien(t *testing.T) {
	state := fakegh.NewState()
	state.Users["ecote"] = "Émilie Côté"
	state.AddRepo("acme", "a26.5n6.01.tp1.ecote", true)
	h := nouveau(t, state)
	place := h.groupe("a26", "5n6", "01", "Émilie Côté", "emilie-cote")
	h.rattacher(place, "emilie-cote", "ecote")

	var liste struct {
		Students []struct {
			Username    string   `json:"username"`
			Accounts    []string `json:"accounts"`
			Assignments []struct {
				Repo string `json:"repo"`
			} `json:"assignments"`
		} `json:"students"`
	}
	h.json(http.MethodGet, "/api/classrooms/"+place+"/students?refresh=1", nil, &liste)
	if len(liste.Students) != 1 {
		t.Fatalf("une seule personne attendue : %+v", liste.Students)
	}
	if strings.Join(liste.Students[0].Accounts, ",") != "emilie-cote,ecote" {
		t.Fatalf("ses deux comptes devraient être rendus : %+v", liste.Students[0])
	}
	if len(liste.Students[0].Assignments) != 1 {
		t.Fatalf("le dépôt sous son autre compte est le sien : %+v", liste.Students[0])
	}
}

// Une équipe nomme ses membres, y compris ceux que seul le registre connaît.
func TestUneEquipeNommeSesMembres(t *testing.T) {
	h, place := groupeAvecEquipes(t)
	var liste listeEquipes
	h.json(http.MethodGet, "/api/classrooms/"+place+"/teams", nil, &liste)
	for _, equipe := range liste.Teams {
		if equipe.Short != "eq1" {
			continue
		}
		noms := make([]string, 0, len(equipe.People))
		for _, personne := range equipe.People {
			noms = append(noms, personne.FullName)
		}
		sort.Strings(noms)
		if strings.Join(noms, ",") != "Jean-Luc Picard,Émilie Côté" {
			t.Fatalf("les membres devraient être nommés : %v", noms)
		}
	}
}

// ------------------------------------------------------- le matricule

// listeAvecMatricules écrit une liste qui porte les matricules et les comptes.
func listeAvecMatricules(lignes ...string) []byte {
	contenu := "no étudiant;nom_complet;github\n" + strings.Join(lignes, "\n") + "\n"
	return []byte(contenu)
}

// Deux comptes qui portent le même matricule sont la même personne : rien n'a
// besoin d'être déclaré, la liste l'a déjà dit.
func TestLeMatriculeReunitDeuxComptes(t *testing.T) {
	state := fakegh.NewState()
	state.Users["ecote"] = "Émilie Côté"
	h := nouveau(t, state)

	var cree struct {
		Scope string `json:"scope"`
	}
	h.json(http.MethodPost, "/api/classrooms", map[string]any{
		"session": "a26", "course": "5n6", "group": "01",
		"students": []map[string]string{
			{"full_name": "Émilie Côté", "username": "emilie-cote", "student_id": "1600105"},
			{"full_name": "Émilie Côté", "username": "ecote", "student_id": "1600105"},
		},
	}, &cree)

	var liste struct {
		Students []struct {
			Username string   `json:"username"`
			Accounts []string `json:"accounts"`
		} `json:"students"`
	}
	h.json(http.MethodGet, "/api/classrooms/"+cree.Scope+"/students", nil, &liste)
	if len(liste.Students) != 1 {
		t.Fatalf("une seule personne attendue : %+v", liste.Students)
	}
	if strings.Join(liste.Students[0].Accounts, ",") != "emilie-cote,ecote" {
		t.Fatalf("ses deux comptes devraient être réunis : %+v", liste.Students[0])
	}

	// Un seul dépôt, et les deux comptes invités.
	h.travail(http.MethodPost, "/api/classrooms/"+cree.Scope+"/assignments",
		map[string]any{"name": "tp1"})
	if noms := h.depots(); strings.Join(noms, ",") != "a26.5n6.01.tp1.emilie-cote" {
		t.Fatalf("un seul dépôt attendu : %v", noms)
	}
	invites := map[string]bool{}
	for _, invitation := range h.State.Invitations["acme/a26.5n6.01.tp1.emilie-cote"] {
		invites[invitation.Login] = true
	}
	if !invites["emilie-cote"] || !invites["ecote"] {
		t.Fatalf("les deux comptes devraient être invités : %v", invites)
	}
}

// Deux matricules différents sont deux personnes, même sous le même nom : leurs
// dépôts porteraient le même nom, et le refus le dit précisément.
func TestDeuxMatriculesRestentDeuxPersonnes(t *testing.T) {
	h := nouveau(t, nil)
	var cree struct {
		Scope string `json:"scope"`
	}
	h.json(http.MethodPost, "/api/classrooms", map[string]any{
		"session": "a26", "course": "5n6", "group": "01",
		"students": []map[string]string{
			{"full_name": "Jean Tremblay", "username": "jtremblay", "student_id": "1111111"},
			{"full_name": "Jean Tremblay", "username": "jean-t", "student_id": "2222222"},
		},
	}, &cree)

	reponse, contenu := h.requete(http.MethodPost,
		"/api/classrooms/"+cree.Scope+"/assignments/preview", map[string]any{"name": "tp1"})
	if reponse.StatusCode != http.StatusBadRequest {
		t.Fatalf("statut %d, attendu 400 — %s", reponse.StatusCode, contenu)
	}
	// Le message nomme les matricules : c'est ce qui prouve que ce sont deux
	// personnes, et ce qui dit quoi faire.
	for _, attendu := range []string{"1111111", "2222222", "Distinguez-les"} {
		if !strings.Contains(string(contenu), attendu) {
			t.Fatalf("message peu explicite : %s", contenu)
		}
	}
}

// Le matricule vient de la liste importée, et suffit ensuite.
func TestLeMatriculeArriveDeLaListe(t *testing.T) {
	state := fakegh.NewState()
	state.Users["ecote"] = "Émilie Côté"
	h := nouveau(t, state)
	place := h.groupe("a26", "5n6", "01")

	var bilan struct {
		Classroom struct {
			Students []struct {
				Username  string `json:"username"`
				StudentID string `json:"student_id"`
			} `json:"students"`
		} `json:"classroom"`
	}
	// La liste se lit comme partout ailleurs : c'est « roster » qui en tire le
	// matricule, et il suit jusqu'à la fiche du groupe.
	var lue struct {
		People []map[string]any `json:"people"`
	}
	h.json(http.MethodPost, "/api/roster/parse", map[string]any{
		"content": listeAvecMatricules(
			"1600105;Émilie Côté;emilie-cote",
			"1600105;Émilie Côté;ecote",
		),
	}, &lue)
	if len(lue.People) != 2 {
		t.Fatalf("deux lignes attendues : %+v", lue.People)
	}
	h.json(http.MethodPost, "/api/classrooms/"+place+"/students",
		map[string]any{"people": lue.People}, &bilan)
	for _, personne := range bilan.Classroom.Students {
		if personne.StudentID != "1600105" {
			t.Fatalf("le matricule devrait suivre : %+v", personne)
		}
	}
}

// Le bug qui a motivé tout ceci : placer quelqu'un dans une équipe où un de ses
// autres comptes se trouve déjà. Le compte demandé doit y entrer, non celui qui
// désigne la personne — sinon la demande se refuse elle-même.
func TestPlacerUnCompteDansUneEquipeOuLAutreEstDeja(t *testing.T) {
	state := fakegh.NewState()
	state.Users["ecote"] = "Émilie Côté"
	h := nouveau(t, state)
	place := h.groupe("a26", "5n6", "01",
		"Émilie Côté", "emilie-cote", "Jean-Luc Picard", "jlpicard")
	h.rattacher(place, "emilie-cote", "ecote")
	h.creerEquipe(place, "eq1", "emilie-cote")

	// Son second compte rejoint la même équipe : elle y travaille sous les deux.
	var bilan listeEquipes
	h.json(http.MethodPost, "/api/classrooms/"+place+"/teams/members",
		map[string]any{"team": "eq1", "usernames": []string{"ecote"}}, &bilan)

	membres := h.State.TeamMembers("acme", fakegh.TeamSlug("a26.5n6.01.eq1"))
	if strings.Join(membres, ",") != "ecote,emilie-cote" {
		t.Fatalf("ses deux comptes devraient être dans l'équipe : %v", membres)
	}
	// Et elle n'y figure qu'une fois : c'est une personne, pas deux.
	for _, equipe := range bilan.Teams {
		if equipe.Short == "eq1" && len(equipe.People) != 1 {
			t.Fatalf("une seule personne attendue dans eq1 : %+v", equipe.People)
		}
	}
}

// Inscrire dans une équipe quelqu'un qui n'est pas membre de l'organisation ne
// l'y met pas : cela l'invite. Il en fait partie pour l'outil, et le taire
// ferait croire que rien ne s'est passé — c'est ce qui arrivait.
func TestUnMembreInviteApparaitQuandMeme(t *testing.T) {
	state := fakegh.NewState()
	state.Users["hugolamarche"] = "Hugo Lamarche"
	// Hugo n'est pas encore membre de l'organisation : GitHub l'invitera.
	state.OutsideOrg["hugolamarche"] = true
	h := nouveau(t, state)
	place := h.groupe("a26", "5n6", "01", "Hugo Lamarche", "hugolamarche")
	h.creerEquipe(place, "voyageurs")

	var bilan listeEquipes
	h.json(http.MethodPost, "/api/classrooms/"+place+"/teams/members",
		map[string]any{"team": "voyageurs", "usernames": []string{"hugolamarche"}}, &bilan)

	// GitHub ne l'a pas mis dans l'équipe : il l'y a invité.
	if membres := h.State.TeamMembers("acme", fakegh.TeamSlug("a26.5n6.01.voyageurs")); len(membres) != 0 {
		t.Fatalf("aucun membre actif attendu : %v", membres)
	}
	invites := h.State.TeamPending("acme", fakegh.TeamSlug("a26.5n6.01.voyageurs"))
	if strings.Join(invites, ",") != "hugolamarche" {
		t.Fatalf("il devrait être invité : %v", invites)
	}

	// L'écran, lui, doit le montrer : il est de l'équipe, en attente.
	var liste listeEquipes
	h.json(http.MethodGet, "/api/classrooms/"+place+"/teams?refresh=1", nil, &liste)
	if len(liste.Teams) != 1 {
		t.Fatalf("une équipe attendue : %+v", liste.Teams)
	}
	if len(liste.Teams[0].People) != 1 ||
		liste.Teams[0].People[0].Username != "hugolamarche" {
		t.Fatalf("Hugo devrait figurer dans l'équipe : %+v", liste.Teams[0])
	}
	if strings.Join(liste.Teams[0].Waiting, ",") != "hugolamarche" {
		t.Fatalf("son attente devrait être dite : %+v", liste.Teams[0].Waiting)
	}
	// Et il ne doit plus être compté « sans équipe ».
	if len(liste.Unassigned) != 0 {
		t.Fatalf("il n'est plus sans équipe : %+v", liste.Unassigned)
	}
}
