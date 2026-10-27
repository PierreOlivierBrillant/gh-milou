package web_test

import (
	"net/http"
	"sort"
	"strings"
	"testing"

	"github.com/PierreOlivierBrillant/gh-milou/internal/cache"
	"github.com/PierreOlivierBrillant/gh-milou/internal/fakegh"
	"github.com/PierreOlivierBrillant/gh-milou/internal/web"
)

// Les équipes de l'interface web, de bout en bout : elles sont créées sur le
// faux GitHub, et c'est son état qu'on interroge ensuite — jamais une fiche
// locale, puisqu'il n'y en a pas.

// ficheEquipe est une équipe telle que l'API la rend.
type ficheEquipe struct {
	Slug      string   `json:"slug"`
	Name      string   `json:"name"`
	Short     string   `json:"short"`
	Members   []string `json:"members"`
	Waiting   []string `json:"waiting"`
	Strangers []struct {
		FullName string `json:"full_name"`
		Username string `json:"username"`
	} `json:"strangers"`
	People []struct {
		FullName string `json:"full_name"`
		Username string `json:"username"`
	} `json:"people"`
}

type listeEquipes struct {
	Teams      []ficheEquipe `json:"teams"`
	Unassigned []struct {
		Username string `json:"username"`
	} `json:"unassigned"`
}

// equipes lit les équipes d'un groupe.
func (h *harnais) equipes(place string) listeEquipes {
	h.t.Helper()
	var liste listeEquipes
	h.json(http.MethodGet, "/api/classrooms/"+place+"/teams", nil, &liste)
	return liste
}

// creerEquipe déclare une équipe et sa composition.
func (h *harnais) creerEquipe(place, nom string, membres ...string) ficheEquipe {
	h.t.Helper()
	var reponse struct {
		Team ficheEquipe `json:"team"`
	}
	h.json(http.MethodPost, "/api/classrooms/"+place+"/teams",
		map[string]any{"name": nom, "members": membres}, &reponse)
	return reponse.Team
}

// groupeAvecEquipes monte le décor commun : trois personnes, deux équipes.
func groupeAvecEquipes(t *testing.T) (*harnais, string) {
	t.Helper()
	h := nouveau(t, nil)
	place := h.groupe("a26", "5n6", "01",
		"Émilie Côté", "emilie-cote",
		"Jean-Luc Picard", "jlpicard",
		"Aminata Diallo", "aminata-d")
	h.creerEquipe(place, "eq1", "emilie-cote", "jlpicard")
	h.creerEquipe(place, "eq2", "aminata-d")
	return h, place
}

// La place du groupe fait partie du nom de l'équipe : c'est ce qui permet à
// deux groupes d'avoir chacun leur « eq1 » dans la même organisation.
func TestUneEquipePorteLaPlaceDeSonGroupe(t *testing.T) {
	h, place := groupeAvecEquipes(t)
	autre := h.groupe("a26", "5n6", "02", "Autre Personne", "autre")
	h.creerEquipe(autre, "eq1", "autre")

	// Le faux GitHub porte d'office des équipes qui ne relèvent d'aucun groupe :
	// seules celles de la nomenclature sont comparées ici.
	noms := make([]string, 0)
	for _, nom := range h.State.TeamNames("acme") {
		if strings.HasPrefix(nom, "a26.") {
			noms = append(noms, nom)
		}
	}
	attendus := []string{"a26.5n6.01.eq1", "a26.5n6.01.eq2", "a26.5n6.02.eq1"}
	if strings.Join(noms, ",") != strings.Join(attendus, ",") {
		t.Fatalf("équipes créées sur GitHub inattendues : %v", noms)
	}
	// Chaque groupe ne voit que les siennes.
	premier := h.equipes(place)
	if len(premier.Teams) != 2 {
		t.Fatalf("le groupe 01 devrait avoir deux équipes : %d", len(premier.Teams))
	}
	second := h.equipes(autre)
	if len(second.Teams) != 1 || second.Teams[0].Short != "eq1" {
		t.Fatalf("le groupe 02 devrait avoir son propre « eq1 » : %+v", second.Teams)
	}
}

// Le nom que GitHub refuserait est refusé avant l'appel, avec un message qui
// parle du nom qu'on a écrit.
func TestDeuxEquipesDuMemeGroupeNePartagentPasUnNom(t *testing.T) {
	h, place := groupeAvecEquipes(t)
	reponse, contenu := h.requete(http.MethodPost, "/api/classrooms/"+place+"/teams",
		map[string]any{"name": "eq1"})
	if reponse.StatusCode != http.StatusBadRequest {
		t.Fatalf("statut attendu 400, reçu %d — %s", reponse.StatusCode, contenu)
	}
	if !strings.Contains(string(contenu), "existe déjà") {
		t.Fatalf("message peu explicite : %s", contenu)
	}
}

func TestLesMembresSontInscritsSurGitHub(t *testing.T) {
	h, _ := groupeAvecEquipes(t)
	membres := h.State.TeamMembers("acme", fakegh.TeamSlug("a26.5n6.01.eq1"))
	if strings.Join(membres, ",") != "emilie-cote,jlpicard" {
		t.Fatalf("membres inattendus : %v", membres)
	}
}

// Déplacer quelqu'un d'une équipe à l'autre le retire de la première : on n'est
// que d'une équipe à la fois dans un groupe.
func TestDeplacerUnEtudiantDUneEquipeALAutre(t *testing.T) {
	h, place := groupeAvecEquipes(t)
	var bilan listeEquipes
	h.json(http.MethodPost, "/api/classrooms/"+place+"/teams/members",
		map[string]any{"team": "eq2", "usernames": []string{"jlpicard"}}, &bilan)

	for _, equipe := range bilan.Teams {
		selon := strings.Join(equipe.Members, ",")
		switch equipe.Short {
		case "eq1":
			if selon != "emilie-cote" {
				t.Fatalf("eq1 devrait avoir perdu Jean-Luc : %v", equipe.Members)
			}
		case "eq2":
			if !strings.Contains(selon, "jlpicard") {
				t.Fatalf("eq2 devrait l'avoir accueilli : %v", equipe.Members)
			}
		}
	}
	// Et sur GitHub, pas seulement dans la réponse.
	if membres := h.State.TeamMembers("acme", fakegh.TeamSlug("a26.5n6.01.eq1")); len(membres) != 1 {
		t.Fatalf("eq1 devrait n'avoir plus qu'un membre sur GitHub : %v", membres)
	}
}

// Quelqu'un d'étranger au groupe n'entre pas dans ses équipes : il aurait accès
// à des dépôts sans qu'aucune liste ne le mentionne.
func TestUneEquipeNAccueillePasQuiNEstPasDuGroupe(t *testing.T) {
	h, place := groupeAvecEquipes(t)
	reponse, contenu := h.requete(http.MethodPost, "/api/classrooms/"+place+"/teams/members",
		map[string]any{"team": "eq1", "usernames": []string{"prof"}})
	if reponse.StatusCode != http.StatusBadRequest {
		t.Fatalf("statut attendu 400, reçu %d — %s", reponse.StatusCode, contenu)
	}
	if !strings.Contains(string(contenu), "inscrivez-le au groupe") {
		t.Fatalf("message peu explicite : %s", contenu)
	}
}

func TestComposerUneEquipeDUnTrait(t *testing.T) {
	h, place := groupeAvecEquipes(t)
	var bilan listeEquipes
	h.json(http.MethodPost, "/api/classrooms/"+place+"/teams/eq1/members",
		map[string]any{"usernames": []string{"emilie-cote", "aminata-d"}}, &bilan)

	membres := h.State.TeamMembers("acme", fakegh.TeamSlug("a26.5n6.01.eq1"))
	if strings.Join(membres, ",") != "aminata-d,emilie-cote" {
		t.Fatalf("composition inattendue : %v", membres)
	}
	// Aminata a quitté eq2 au passage, et Jean-Luc n'est plus dans aucune.
	if reste := h.State.TeamMembers("acme", fakegh.TeamSlug("a26.5n6.01.eq2")); len(reste) != 0 {
		t.Fatalf("eq2 devrait être vide : %v", reste)
	}
	if len(bilan.Unassigned) != 1 || bilan.Unassigned[0].Username != "jlpicard" {
		t.Fatalf("Jean-Luc devrait être sans équipe : %+v", bilan.Unassigned)
	}
}

func TestRetirerQuelquUnDeSonEquipeLeLaisseDansLeGroupe(t *testing.T) {
	h, place := groupeAvecEquipes(t)
	var bilan listeEquipes
	h.json(http.MethodDelete,
		"/api/classrooms/"+place+"/teams/eq1/members/jlpicard", nil, &bilan)
	if len(bilan.Unassigned) != 1 || bilan.Unassigned[0].Username != "jlpicard" {
		t.Fatalf("il devrait rester du groupe, sans équipe : %+v", bilan.Unassigned)
	}
}

func TestRenommerUneEquipeGardeSaPlaceEtSesMembres(t *testing.T) {
	h, place := groupeAvecEquipes(t)
	var reponse struct {
		Team     ficheEquipe `json:"team"`
		Previous string      `json:"previous"`
	}
	h.json(http.MethodPut, "/api/classrooms/"+place+"/teams/eq1",
		map[string]any{"name": "rouge"}, &reponse)

	if reponse.Team.Name != "a26.5n6.01.rouge" || reponse.Previous != "eq1" {
		t.Fatalf("renommage inattendu : %+v", reponse)
	}
	membres := h.State.TeamMembers("acme", fakegh.TeamSlug("a26.5n6.01.rouge"))
	if strings.Join(membres, ",") != "emilie-cote,jlpicard" {
		t.Fatalf("l'équipe devrait garder ses membres : %v", membres)
	}
}

// Supprimer une équipe ne touche à aucun dépôt : c'est l'accès qui disparaît.
func TestSupprimerUneEquipeLaisseSesDepots(t *testing.T) {
	h, place := groupeAvecEquipes(t)
	h.State.AddRepo("acme", "a26.5n6.01.projet.eq1", true)

	var bilan map[string]any
	h.json(http.MethodDelete, "/api/classrooms/"+place+"/teams/eq1", nil, &bilan)
	if !strings.Contains(bilan["message"].(string), "dépôts restent") {
		t.Fatalf("le message devrait rassurer sur les dépôts : %v", bilan["message"])
	}
	if noms := h.depots(); len(noms) != 1 {
		t.Fatalf("le dépôt devrait subsister : %v", noms)
	}
	if len(h.equipes(place).Teams) != 1 {
		t.Fatal("l'équipe devrait avoir disparu")
	}
}

// Déplacer une équipe, c'est déplacer les trois : l'équipe, ses membres, et
// tout ce que les uns comme l'autre ont rendu. Le reste du groupe ne bouge pas.
func TestDeplacerUneEquipeVersUnAutreGroupe(t *testing.T) {
	h, place := groupeAvecEquipes(t)
	h.State.AddRepo("acme", "a26.5n6.01.projet.eq1", true)
	h.State.AddRepo("acme", "a26.5n6.01.tp1.emilie-cote", true)
	h.State.AddRepo("acme", "a26.5n6.01.tp1.jean-luc-picard", true)
	h.State.AddRepo("acme", "a26.5n6.01.tp1.aminata-diallo", true)

	bilan := h.travail(http.MethodPost, "/api/classrooms/"+place+"/teams/eq1/move",
		map[string]any{"new_group": map[string]string{
			"session": "a26", "course": "5n6", "group": "02"}})
	resultat, _ := bilan["result"].(map[string]any)
	if resultat == nil {
		t.Fatalf("aucun bilan : %v", bilan)
	}
	if resultat["failed"] != float64(0) {
		t.Fatalf("des renommages ont échoué : %v", resultat)
	}

	// Les dépôts de l'équipe et de ses membres portent la place d'arrivée ;
	// celui d'Aminata, qui n'en est pas, n'a pas bougé.
	attendus := []string{
		"a26.5n6.01.tp1.aminata-diallo",
		"a26.5n6.02.projet.eq1",
		"a26.5n6.02.tp1.emilie-cote",
		"a26.5n6.02.tp1.jean-luc-picard",
	}
	if noms := h.depots(); strings.Join(noms, ",") != strings.Join(attendus, ",") {
		t.Fatalf("dépôts après déplacement : %v", noms)
	}

	// L'équipe elle-même porte la place de son nouveau groupe : c'est son nom
	// qui dit à quel groupe elle appartient.
	var arrivee listeEquipes
	h.json(http.MethodGet, "/api/classrooms/a26.5n6.02/teams?refresh=1", nil, &arrivee)
	if len(arrivee.Teams) != 1 || arrivee.Teams[0].Name != "a26.5n6.02.eq1" {
		t.Fatalf("l'équipe devrait être arrivée : %+v", arrivee.Teams)
	}
	comptes := make([]string, 0, 2)
	for _, personne := range arrivee.Teams[0].People {
		comptes = append(comptes, personne.Username)
	}
	sort.Strings(comptes)
	if strings.Join(comptes, ",") != "emilie-cote,jlpicard" {
		t.Fatalf("ses membres devraient l'avoir suivie : %v", comptes)
	}
	// Et ils ont quitté le groupe de départ.
	if len(h.equipes(place).Teams) != 1 {
		t.Fatalf("le départ ne devrait garder qu'eq2")
	}
}

// Une équipe ne peut pas arriver là où son nom court est déjà pris : deux
// équipes d'un même groupe ne peuvent pas le partager.
func TestUneEquipeNArrivePasSurUnNomDejaPris(t *testing.T) {
	h, place := groupeAvecEquipes(t)
	ailleurs := h.groupe("a26", "5n6", "02", "Karim Bélanger", "kbelanger")
	h.creerEquipe(ailleurs, "eq1", "kbelanger")

	reponse, contenu := h.requete(http.MethodPost,
		"/api/classrooms/"+place+"/teams/eq1/move", map[string]any{"target": ailleurs})
	if reponse.StatusCode < 300 {
		t.Fatal("un nom déjà pris à l'arrivée doit être refusé")
	}
	if !strings.Contains(string(contenu), "existe déjà") {
		t.Fatalf("le refus devrait le dire : %s", contenu)
	}
}

// Le dépôt d'une équipe dit qui la compose, nom et compte, et signale celui
// qui a été invité sans avoir encore accepté : c'est la même lecture que dans
// l'onglet des équipes, et c'est là qu'on vient la vérifier.
func TestLeDepotDUneEquipeNommeSesMembresEtCeuxQuiAttendent(t *testing.T) {
	state := fakegh.NewState()
	state.Users["hugolevacher"] = "Hugo Levacher"
	state.OutsideOrg["hugolevacher"] = true
	h := nouveau(t, state)
	place := h.groupe("a26", "5n6", "01",
		"Émilie Côté", "emilie-cote", "Hugo Levacher", "hugolevacher")
	h.creerEquipe(place, "eq1", "emilie-cote", "hugolevacher")
	h.State.AddRepo("acme", "a26.5n6.01.projet.eq1", true)

	var detail struct {
		Repos []struct {
			Team    string   `json:"team"`
			Waiting []string `json:"waiting"`
			Members []struct {
				FullName string `json:"full_name"`
				Username string `json:"username"`
			} `json:"members"`
		} `json:"repos"`
	}
	h.json(http.MethodGet, "/api/classrooms/"+place+"/assignments/projet", nil, &detail)
	if len(detail.Repos) != 1 || detail.Repos[0].Team != "eq1" {
		t.Fatalf("un dépôt d'équipe attendu : %+v", detail.Repos)
	}
	noms := make([]string, 0, 2)
	for _, membre := range detail.Repos[0].Members {
		noms = append(noms, membre.FullName+" (@"+membre.Username+")")
	}
	sort.Strings(noms)
	if strings.Join(noms, ", ") != "Hugo Levacher (@hugolevacher), Émilie Côté (@emilie-cote)" {
		t.Fatalf("les membres devraient être nommés : %v", noms)
	}
	if strings.Join(detail.Repos[0].Waiting, ",") != "hugolevacher" {
		t.Fatalf("l'invitation en attente devrait être dite : %v", detail.Repos[0].Waiting)
	}
}

// Supprimer les dépôts avec l'équipe se demande, et se confirme : ceux des
// autres équipes restent, et un nom mal retapé ne détruit rien.
func TestSupprimerUneEquipeAvecSesDepots(t *testing.T) {
	h, place := groupeAvecEquipes(t)
	h.State.AddRepo("acme", "a26.5n6.01.projet.eq1", true)
	h.State.AddRepo("acme", "a26.5n6.01.tp1.eq1", true)
	h.State.AddRepo("acme", "a26.5n6.01.projet.eq2", true)

	reponse, _ := h.requete(http.MethodDelete, "/api/classrooms/"+place+"/teams/eq1",
		map[string]any{"repos": true, "confirm": "eq2"})
	if reponse.StatusCode < 300 {
		t.Fatal("un nom mal retapé doit être refusé")
	}
	if noms := h.depots(); len(noms) != 3 {
		t.Fatalf("un refus ne supprime rien : %v", noms)
	}

	var bilan map[string]any
	h.json(http.MethodDelete, "/api/classrooms/"+place+"/teams/eq1",
		map[string]any{"repos": true, "confirm": "eq1"}, &bilan)
	if noms := h.depots(); len(noms) != 1 || noms[0] != "a26.5n6.01.projet.eq2" {
		t.Fatalf("seuls les dépôts d'eq1 devaient partir : %v", noms)
	}
	if !strings.Contains(bilan["message"].(string), "ses 2 dépôts") {
		t.Fatalf("le message devrait dire combien : %v", bilan["message"])
	}
	if len(h.equipes(place).Teams) != 1 {
		t.Fatal("l'équipe devrait avoir disparu")
	}
}

// La liste des équipes dit combien de dépôts chacune a rendus : c'est ce que la
// case à cocher de la suppression emporterait, et on ne le coche pas à
// l'aveugle.
func TestLaListeDesEquipesCompteLeursDepots(t *testing.T) {
	h, place := groupeAvecEquipes(t)
	h.State.AddRepo("acme", "a26.5n6.01.projet.eq1", true)
	h.State.AddRepo("acme", "a26.5n6.01.tp1.eq1", true)

	var liste struct {
		Repos map[string]int `json:"repos"`
	}
	h.json(http.MethodGet, "/api/classrooms/"+place+"/teams", nil, &liste)
	if liste.Repos["eq1"] != 2 {
		t.Fatalf("eq1 a rendu deux dépôts : %v", liste.Repos)
	}
	if _, compte := liste.Repos["eq2"]; compte {
		t.Fatalf("eq2 n'a rien rendu : %v", liste.Repos)
	}
}

// Adopter, c'est renommer : l'équipe garde ses membres, ses accès et son
// histoire, et devient lisible pour l'outil.
func TestAdopterUneEquipeExistante(t *testing.T) {
	state := fakegh.NewState()
	state.AddTeam("acme", "Les anciens", "emilie-cote", "jlpicard")
	h := nouveau(t, state)
	place := h.groupe("a26", "5n6", "01",
		"Émilie Côté", "emilie-cote", "Jean-Luc Picard", "jlpicard")

	var libres struct {
		Teams []ficheEquipe `json:"teams"`
	}
	h.json(http.MethodGet, "/api/orgs/acme/teams", nil, &libres)
	slug := ""
	for _, equipe := range libres.Teams {
		if equipe.Name == "Les anciens" {
			slug = equipe.Slug
		}
	}
	if slug == "" {
		t.Fatalf("l'équipe libre devrait être proposée : %+v", libres.Teams)
	}

	var adoptee struct {
		Team     ficheEquipe `json:"team"`
		Previous string      `json:"previous"`
	}
	h.json(http.MethodPost, "/api/classrooms/"+place+"/teams/adopt",
		map[string]any{"slug": slug, "name": "eq1"}, &adoptee)
	if adoptee.Team.Name != "a26.5n6.01.eq1" || adoptee.Previous != "Les anciens" {
		t.Fatalf("adoption inattendue : %+v", adoptee)
	}
	membres := h.State.TeamMembers("acme", fakegh.TeamSlug("a26.5n6.01.eq1"))
	if strings.Join(membres, ",") != "emilie-cote,jlpicard" {
		t.Fatalf("l'équipe adoptée garde ses membres : %v", membres)
	}
}

// -------------------------------------------------------- travaux d'équipe

func TestDistribuerUnTravailDEquipe(t *testing.T) {
	h, place := groupeAvecEquipes(t)
	bilan := h.travail(http.MethodPost, "/api/classrooms/"+place+"/assignments",
		map[string]any{"name": "projet", "teams": true})
	if bilan["status"] != "terminé" {
		t.Fatalf("distribution en échec : %v", bilan)
	}

	noms := h.depots()
	sort.Strings(noms)
	attendus := []string{"a26.5n6.01.projet.eq1", "a26.5n6.01.projet.eq2"}
	if strings.Join(noms, ",") != strings.Join(attendus, ",") {
		t.Fatalf("un dépôt par équipe attendu : %v", noms)
	}

	// L'accès est accordé à l'équipe, pas à ses membres : c'est ce qui fait
	// qu'un changement de composition suffit ensuite à changer qui y accède.
	partages := h.State.TeamRepoNames("acme", fakegh.TeamSlug("a26.5n6.01.eq1"))
	if strings.Join(partages, ",") != "acme/a26.5n6.01.projet.eq1" {
		t.Fatalf("le dépôt devrait être partagé avec eq1 : %v", partages)
	}
	if invitations := h.State.Invitations["acme/a26.5n6.01.projet.eq1"]; len(invitations) != 0 {
		t.Fatalf("personne ne devrait être invité individuellement : %v", invitations)
	}
}

// Rien n'oblige à servir toutes les équipes d'un coup : une équipe se forme
// parfois après les autres.
func TestDistribuerAQuelquesEquipesSeulement(t *testing.T) {
	h, place := groupeAvecEquipes(t)
	h.travail(http.MethodPost, "/api/classrooms/"+place+"/assignments",
		map[string]any{"name": "projet", "teams": true, "team_names": []string{"eq1"}})

	if noms := h.depots(); strings.Join(noms, ",") != "a26.5n6.01.projet.eq1" {
		t.Fatalf("seule eq1 devait être servie : %v", noms)
	}

	// La seconde distribution écarte celle qui a déjà son dépôt.
	var apercu struct {
		Items  []map[string]any `json:"items"`
		Served []string         `json:"served"`
		Teams  bool             `json:"teams"`
	}
	h.json(http.MethodPost, "/api/classrooms/"+place+"/assignments/preview",
		map[string]any{"name": "projet", "teams": true}, &apercu)
	if !apercu.Teams || len(apercu.Items) != 1 {
		t.Fatalf("une seule équipe reste à servir : %+v", apercu)
	}
	if len(apercu.Served) != 1 || apercu.Served[0] != "eq1" {
		t.Fatalf("eq1 devrait être écartée : %v", apercu.Served)
	}
}

// La nature d'un travail se lit dans le nom de ses dépôts : ni l'un ni l'autre
// n'a été déclaré nulle part.
func TestLaNatureDUnTravailSeLitApresCoup(t *testing.T) {
	h, place := groupeAvecEquipes(t)
	h.travail(http.MethodPost, "/api/classrooms/"+place+"/assignments",
		map[string]any{"name": "projet", "teams": true})
	h.travail(http.MethodPost, "/api/classrooms/"+place+"/assignments",
		map[string]any{"name": "tp1"})

	var fiche struct {
		Teams       int `json:"teams"`
		Assignments []struct {
			Name  string `json:"name"`
			Kind  string `json:"kind"`
			Teams int    `json:"teams"`
		} `json:"assignments"`
	}
	h.json(http.MethodGet, "/api/classrooms/"+place+"?refresh=1", nil, &fiche)
	if fiche.Teams != 2 {
		t.Fatalf("le groupe devrait annoncer ses deux équipes : %+v", fiche)
	}
	natures := map[string]string{}
	for _, travail := range fiche.Assignments {
		natures[travail.Name] = travail.Kind
	}
	if natures["projet"] != "équipe" || natures["tp1"] != "individuel" {
		t.Fatalf("natures mal relues : %v", natures)
	}
}

// Un travail d'équipe se distribue sans que les noms complets soient connus :
// c'est l'équipe qui nomme le dépôt.
func TestUnTravailDEquipeNExigePasLesNomsComplets(t *testing.T) {
	h := nouveau(t, nil)
	// Une liste sans nom complet : celle d'un groupe adopté depuis des dépôts
	// qui ne portaient que des comptes GitHub.
	place := h.groupe("a26", "5n6", "01", "", "emilie-cote", "", "jlpicard")
	h.creerEquipe(place, "eq1", "emilie-cote", "jlpicard")

	reponse, contenu := h.requete(http.MethodPost,
		"/api/classrooms/"+place+"/assignments/preview", map[string]any{"name": "tp1"})
	if reponse.StatusCode != http.StatusBadRequest {
		t.Fatalf("un travail individuel exige les noms complets : %d — %s",
			reponse.StatusCode, contenu)
	}

	var apercu struct {
		Items []struct {
			Name string `json:"name"`
		} `json:"items"`
	}
	h.json(http.MethodPost, "/api/classrooms/"+place+"/assignments/preview",
		map[string]any{"name": "projet", "teams": true}, &apercu)
	if len(apercu.Items) != 1 || apercu.Items[0].Name != "a26.5n6.01.projet.eq1" {
		t.Fatalf("le dépôt devrait porter le nom de l'équipe : %+v", apercu.Items)
	}
}

// La liste des groupes reconnaît un travail d'équipe au nom de ses dépôts : le
// nom des équipes suffit, et leur composition — deux requêtes par équipe — n'est
// pas lue pour un écran qui ne la montre pas.
func TestLaListeDesGroupesNeLitPasLaCompositionDesEquipes(t *testing.T) {
	state := fakegh.NewState()
	state.AddRepo("acme", "a26.5n6.01.tp1.emilie-cote", true)
	state.AddRepo("acme", "a26.5n6.01.projet.eq1", true)
	state.AddTeam("acme", "a26.5n6.01.eq1", "emilie-cote", "jlpicard")
	// Sans cache, rien ne se précharge en arrière-plan : les seules requêtes
	// comptées sont celles de la liste.
	h := nouveauAvec(t, state, func(deps *web.Deps) {
		deps.Cache = cache.NewIn(t.TempDir(), false)
	})

	var liste struct {
		Classrooms []struct {
			Scope       string `json:"scope"`
			Teams       int    `json:"teams"`
			Assignments []struct {
				Name string `json:"name"`
				Kind string `json:"kind"`
			} `json:"assignments"`
		} `json:"classrooms"`
	}
	h.json(http.MethodGet, "/api/classrooms", nil, &liste)

	if len(liste.Classrooms) != 1 || liste.Classrooms[0].Teams != 1 {
		t.Fatalf("liste = %+v", liste.Classrooms)
	}
	natures := map[string]string{}
	for _, travail := range liste.Classrooms[0].Assignments {
		natures[travail.Name] = travail.Kind
	}
	if natures["projet"] != "équipe" || natures["tp1"] != "individuel" {
		t.Errorf("natures = %v", natures)
	}
	equipe := "/orgs/acme/teams/" + fakegh.TeamSlug("a26.5n6.01.eq1")
	if lus := state.CallCount(equipe+"/members") + state.CallCount(equipe+"/invitations"); lus != 0 {
		t.Errorf("%d lecture(s) de la composition d'une équipe pour lister les groupes", lus)
	}
}

// Adopter un travail fait en équipe avant l'outil : les dépôts sont déjà là et
// bien nommés, mais rien ne les a jamais partagés avec l'équipe.
func TestPartagerLesDepotsDunTravailAdopte(t *testing.T) {
	h, place := groupeAvecEquipes(t)
	h.State.AddRepo("acme", "a26.5n6.01.projet.eq1", true)
	h.State.AddRepo("acme", "a26.5n6.01.projet.eq2", true)

	var travaux struct {
		Assignments []struct {
			Name string `json:"name"`
			Kind string `json:"kind"`
		} `json:"assignments"`
	}
	h.json(http.MethodGet, "/api/classrooms/"+place+"?refresh=1", nil, &travaux)
	if len(travaux.Assignments) != 1 || travaux.Assignments[0].Kind != "équipe" {
		t.Fatalf("le travail devrait être reconnu comme d'équipe : %+v", travaux.Assignments)
	}

	bilan := h.travail(http.MethodPost,
		"/api/classrooms/"+place+"/assignments/projet/share", nil)
	resultat, _ := bilan["result"].(map[string]any)
	if resultat == nil || resultat["shared"].(float64) != 2 {
		t.Fatalf("les deux dépôts devraient être partagés : %v", bilan)
	}
	for _, equipe := range []string{"eq1", "eq2"} {
		slug := fakegh.TeamSlug("a26.5n6.01." + equipe)
		if depots := h.State.TeamRepoNames("acme", slug); len(depots) != 1 {
			t.Fatalf("%s devrait voir son dépôt : %v", equipe, depots)
		}
	}
}

// Une équipe libre de tout groupe n'appartient à aucun : elle reste à adopter,
// et n'apparaît dans les équipes d'aucun groupe.
func TestUneEquipeLibreNAppartientAAucunGroupe(t *testing.T) {
	state := fakegh.NewState()
	state.AddTeam("acme", "Les anciens", "emilie-cote")
	h := nouveau(t, state)
	place := h.groupe("a26", "5n6", "01", "Émilie Côté", "emilie-cote")

	if liste := h.equipes(place); len(liste.Teams) != 0 {
		t.Fatalf("le groupe ne devrait avoir aucune équipe : %+v", liste.Teams)
	}
	var libres struct {
		Teams []ficheEquipe `json:"teams"`
	}
	h.json(http.MethodGet, "/api/orgs/acme/teams", nil, &libres)
	noms := make([]string, 0, len(libres.Teams))
	for _, equipe := range libres.Teams {
		noms = append(noms, equipe.Name)
	}
	sort.Strings(noms)
	// Les équipes que le faux GitHub porte d'office s'y trouvent aussi : rien
	// ne les rattache à un groupe.
	if strings.Join(noms, ",") != "Les anciens,direction,enseignants" {
		t.Fatalf("équipes libres = %v", noms)
	}
}
