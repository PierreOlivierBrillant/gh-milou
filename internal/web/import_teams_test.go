package web_test

import (
	"net/http"
	"sort"
	"strings"
	"testing"

	"github.com/PierreOlivierBrillant/gh-milou/internal/fakegh"
)

// Reprendre un travail fait en équipe : ce qui suit le préfixe nomme une
// équipe, et ce sont les accès du dépôt qui disent qui en est.

// equipeOrg monte une organisation où un travail d'équipe a été fait sans
// l'outil : deux dépôts, deux ou trois personnes dans chacun.
func equipeOrg() *fakegh.State {
	state := fakegh.NewState()
	state.Users["wasim"] = "Wasim Hakkani"
	state.Users["edouard-l"] = "Édouard Loiselle"
	for nom, membres := range map[string][]string{
		"projet-alpha": {"emilie-cote", "jlpicard"},
		"projet-beta":  {"aminata-d", "wasim", "edouard-l"},
	} {
		state.AddRepo("acme", nom, true)
		for _, compte := range membres {
			state.AddCollaborator("acme/"+nom, compte, "push")
		}
		// L'enseignant a accès à tout : il ne fait partie d'aucune équipe.
		state.AddCollaborator("acme/"+nom, "prof", "admin")
	}
	return state
}

// repriseEquipe est ce que l'aperçu d'une reprise en équipe rend.
type repriseEquipe struct {
	TeamWork bool   `json:"team_work"`
	Prefix   string `json:"prefix"`
	Name     string `json:"name"`
	Scope    string `json:"scope"`
	Teams    []struct {
		Short   string            `json:"short"`
		Name    string            `json:"name"`
		Repo    string            `json:"repo"`
		Target  string            `json:"target"`
		Members []string          `json:"members"`
		Sources map[string]string `json:"sources"`
		Exists  bool              `json:"exists"`
	} `json:"teams"`
	Moves []struct {
		Repo   string `json:"repo"`
		Target string `json:"target"`
	} `json:"moves"`
	Students []struct {
		FullName string `json:"full_name"`
		Username string `json:"username"`
	} `json:"students"`
	Pairings []struct {
		Login string `json:"login"`
		Entry struct {
			FullName string `json:"full_name"`
		} `json:"entry"`
	} `json:"pairings"`
	Unmatched []string `json:"unmatched"`
	Absent    []string `json:"absent"`
	Silent    []string `json:"silent"`
}

// apercuEquipe demande l'aperçu d'une reprise en équipe.
func (h *harnais) apercuEquipe(corps map[string]any) repriseEquipe {
	h.t.Helper()
	corps["teams"] = true
	var vue repriseEquipe
	h.json(http.MethodPost, "/api/orgs/acme/import/preview", corps, &vue)
	return vue
}

// L'aperçu lit l'équipe dans le nom, et ses membres dans les accès.
func TestRepriseEnEquipeLitLesMembresDansLesAcces(t *testing.T) {
	h := nouveau(t, equipeOrg())
	vue := h.apercuEquipe(map[string]any{
		"prefix": "projet", "name": "projet", "scope": "a26.5n6.01",
	})

	if !vue.TeamWork || len(vue.Teams) != 2 {
		t.Fatalf("deux équipes attendues : %+v", vue)
	}
	trouve := map[string][]string{}
	cibles := map[string]string{}
	for _, equipe := range vue.Teams {
		trouve[equipe.Short] = equipe.Members
		cibles[equipe.Short] = equipe.Target
		if equipe.Exists {
			t.Fatalf("aucune équipe n'existe encore : %+v", equipe)
		}
	}
	if strings.Join(trouve["alpha"], ",") != "emilie-cote,jlpicard" {
		t.Fatalf("membres d'alpha = %v", trouve["alpha"])
	}
	if strings.Join(trouve["beta"], ",") != "aminata-d,edouard-l,wasim" {
		t.Fatalf("membres de beta = %v", trouve["beta"])
	}
	// L'enseignant a accès à tout : il n'est d'aucune équipe.
	for court, membres := range trouve {
		for _, compte := range membres {
			if compte == "prof" {
				t.Fatalf("l'enseignant ne devrait pas être dans « %s »", court)
			}
		}
	}
	// Le dépôt prend le nom de son équipe au dernier niveau.
	if cibles["alpha"] != "a26.5n6.01.projet.alpha" {
		t.Fatalf("cible d'alpha = %s", cibles["alpha"])
	}
	if len(vue.Moves) != 2 {
		t.Fatalf("deux renommages attendus : %+v", vue.Moves)
	}
	// Les membres deviennent les étudiants du groupe, nommés quand
	// l'organisation les connaît.
	if len(vue.Students) != 5 {
		t.Fatalf("cinq personnes attendues : %+v", vue.Students)
	}
}

// La reprise renomme les dépôts, crée les équipes, et leur partage le leur.
func TestRepriseEnEquipeReconstitueLesEquipes(t *testing.T) {
	h := nouveau(t, equipeOrg())
	bilan := h.travail(http.MethodPost, "/api/orgs/acme/import", map[string]any{
		"prefix": "projet", "name": "projet", "scope": "a26.5n6.01", "teams": true,
	})
	if bilan["status"] != "terminé" {
		t.Fatalf("reprise en échec : %v", bilan)
	}
	resultat, _ := bilan["result"].(map[string]any)
	if resultat == nil || resultat["renamed"].(float64) != 2 ||
		resultat["teams"].(float64) != 2 || resultat["shared"].(float64) != 2 {
		t.Fatalf("bilan inattendu : %v", resultat)
	}

	noms := h.depots()
	sort.Strings(noms)
	attendus := []string{"a26.5n6.01.projet.alpha", "a26.5n6.01.projet.beta"}
	if strings.Join(noms, ",") != strings.Join(attendus, ",") {
		t.Fatalf("dépôts renommés inattendus : %v", noms)
	}

	// Les équipes portent la place du groupe, et leurs membres viennent des
	// accès des dépôts.
	membres := h.State.TeamMembers("acme", fakegh.TeamSlug("a26.5n6.01.alpha"))
	if strings.Join(membres, ",") != "emilie-cote,jlpicard" {
		t.Fatalf("membres d'alpha = %v", membres)
	}
	// Chaque équipe a reçu son dépôt.
	partages := h.State.TeamRepoNames("acme", fakegh.TeamSlug("a26.5n6.01.beta"))
	if strings.Join(partages, ",") != "acme/a26.5n6.01.projet.beta" {
		t.Fatalf("dépôt partagé avec beta = %v", partages)
	}

	// Et le travail se relit comme s'il avait toujours été distribué ainsi.
	var fiche struct {
		Teams       int `json:"teams"`
		Assignments []struct {
			Name  string `json:"name"`
			Kind  string `json:"kind"`
			Teams int    `json:"teams"`
		} `json:"assignments"`
	}
	h.json(http.MethodGet, "/api/classrooms/a26.5n6.01?refresh=1", nil, &fiche)
	if fiche.Teams != 2 || len(fiche.Assignments) != 1 {
		t.Fatalf("le groupe devrait porter deux équipes et un travail : %+v", fiche)
	}
	if fiche.Assignments[0].Kind != "équipe" || fiche.Assignments[0].Teams != 2 {
		t.Fatalf("le travail devrait être d'équipe : %+v", fiche.Assignments[0])
	}
}

// Une équipe déjà là n'est pas recréée : la reprise la complète.
func TestRepriseEnEquipeCompleteUneEquipeDejaLa(t *testing.T) {
	state := equipeOrg()
	state.AddTeam("acme", "a26.5n6.01.alpha", "emilie-cote")
	h := nouveau(t, state)

	vue := h.apercuEquipe(map[string]any{
		"prefix": "projet", "scope": "a26.5n6.01",
	})
	for _, equipe := range vue.Teams {
		if equipe.Short != "alpha" {
			continue
		}
		if !equipe.Exists {
			t.Fatalf("alpha existe déjà : %+v", equipe)
		}
		if strings.Join(equipe.Members, ",") != "emilie-cote,jlpicard" {
			t.Fatalf("membres d'alpha = %v", equipe.Members)
		}
	}

	h.travail(http.MethodPost, "/api/orgs/acme/import", map[string]any{
		"prefix": "projet", "scope": "a26.5n6.01", "teams": true,
	})
	if noms := h.State.TeamNames("acme"); len(noms) != 4 {
		// « enseignants » et « direction » du décor, plus alpha et beta.
		t.Fatalf("une seule équipe devait naître : %v", noms)
	}
}

// Un dépôt dont les accès ne disent rien donne une équipe vide : le dépôt lui
// appartient, mais il reste à dire qui en est.
func TestRepriseEnEquipeSignaleUnDepotSansAcces(t *testing.T) {
	state := fakegh.NewState()
	state.AddRepo("acme", "projet-alpha", true)
	h := nouveau(t, state)

	vue := h.apercuEquipe(map[string]any{"prefix": "projet", "scope": "a26.5n6.01"})
	if len(vue.Silent) != 1 || vue.Silent[0] != "projet-alpha" {
		t.Fatalf("le dépôt muet devrait être signalé : %+v", vue.Silent)
	}
	if len(vue.Teams) != 1 || len(vue.Teams[0].Members) != 0 {
		t.Fatalf("l'équipe devrait être vide : %+v", vue.Teams)
	}
}

// Deux dépôts qui donneraient la même équipe arrêtent la reprise : « alpha » et
// « Alpha » ne font qu'un nom sur GitHub.
func TestRepriseEnEquipeRefuseDeuxDepotsPourUneEquipe(t *testing.T) {
	state := fakegh.NewState()
	state.AddRepo("acme", "projet-alpha", true)
	state.AddRepo("acme", "projet-Alpha", true)
	h := nouveau(t, state)

	reponse, contenu := h.requete(http.MethodPost, "/api/orgs/acme/import/preview",
		map[string]any{"prefix": "projet", "scope": "a26.5n6.01", "teams": true})
	if reponse.StatusCode != http.StatusBadRequest {
		t.Fatalf("statut attendu 400, reçu %d — %s", reponse.StatusCode, contenu)
	}
	if !strings.Contains(string(contenu), "même équipe") {
		t.Fatalf("message peu explicite : %s", contenu)
	}
}

// La sélection vaut aussi pour une reprise en équipe : un dépôt d'essai reste
// où il est.
func TestRepriseEnEquipeRespecteLaSelection(t *testing.T) {
	h := nouveau(t, equipeOrg())
	vue := h.apercuEquipe(map[string]any{
		"prefix": "projet", "scope": "a26.5n6.01",
		"only": []string{"projet-alpha"},
	})
	if len(vue.Teams) != 1 || vue.Teams[0].Short != "alpha" {
		t.Fatalf("seule alpha devait être reprise : %+v", vue.Teams)
	}
	if len(vue.Moves) != 1 {
		t.Fatalf("un seul renommage attendu : %+v", vue.Moves)
	}
}

// ------------------------------------------ d'où viennent les membres

// classroomEquipes monte une organisation telle que GitHub Classroom laisse un
// travail d'équipe : le dépôt n'a aucun collaborateur direct, il est partagé
// avec une équipe.
func classroomEquipes() *fakegh.State {
	state := fakegh.NewState()
	state.Users["wasim"] = "Wasim Hakkani"
	state.AddRepo("acme", "backend-castors", true)
	state.AddRepo("acme", "backend-mccoy", true)
	state.AddTeam("acme", "Castors", "emilie-cote", "jlpicard")
	state.AddTeam("acme", "McCoy", "aminata-d", "wasim")
	state.ShareRepo("acme", fakegh.TeamSlug("Castors"), "acme/backend-castors", "push")
	state.ShareRepo("acme", fakegh.TeamSlug("McCoy"), "acme/backend-mccoy", "push")
	// L'équipe enseignante voit tout : elle n'est l'équipe de personne.
	state.ShareRepo("acme", "enseignants", "acme/backend-castors", "push")
	state.ShareRepo("acme", "enseignants", "acme/backend-mccoy", "push")
	return state
}

// Le cas de GitHub Classroom : aucun collaborateur direct, mais une équipe
// GitHub par dépôt. C'est là qu'il faut aller chercher les membres.
func TestRepriseEnEquipeLitLEquipeGitHubDuDepot(t *testing.T) {
	state := classroomEquipes()
	state.AddTeam("acme", "enseignants-ignores") // pour ne pas confondre les slugs
	h := nouveau(t, state)

	vue := h.apercuEquipe(map[string]any{"prefix": "backend", "scope": "a25.5w5.1010"})
	trouve := map[string][]string{}
	sources := map[string]map[string]string{}
	for _, equipe := range vue.Teams {
		trouve[equipe.Short] = equipe.Members
		sources[equipe.Short] = equipe.Sources
	}
	if strings.Join(trouve["castors"], ",") != "emilie-cote,jlpicard" {
		t.Fatalf("membres de castors = %v", trouve["castors"])
	}
	if strings.Join(trouve["mccoy"], ",") != "aminata-d,wasim" {
		t.Fatalf("membres de mccoy = %v", trouve["mccoy"])
	}
	// Et l'écran doit pouvoir dire d'où ils viennent.
	if sources["castors"]["emilie-cote"] != "équipe" {
		t.Fatalf("sources de castors = %v", sources["castors"])
	}
	if len(vue.Silent) != 0 {
		t.Fatalf("aucune équipe ne devrait être vide : %v", vue.Silent)
	}
}

// Une équipe que plusieurs dépôts du travail partagent n'est l'équipe de
// personne : c'est celle qui enseigne, et ses membres n'ont rien à faire dans
// les équipes d'étudiants.
func TestRepriseEnEquipeEcarteLEquipeEnseignante(t *testing.T) {
	state := classroomEquipes()
	// L'équipe enseignante a un membre bien à elle.
	state.Teams["acme/enseignants"].Members["correcteur"] = "member"
	state.Users["correcteur"] = "Correctrice"
	h := nouveau(t, state)

	vue := h.apercuEquipe(map[string]any{"prefix": "backend", "scope": "a25.5w5.1010"})
	for _, equipe := range vue.Teams {
		for _, compte := range equipe.Members {
			if compte == "correcteur" {
				t.Fatalf("« %s » ne devrait pas recevoir l'équipe enseignante : %v",
					equipe.Short, equipe.Members)
			}
		}
	}
}

// Quand ni l'équipe ni les accès ne disent rien, ceux qui ont poussé du code
// le disent.
func TestRepriseEnEquipeLitLesAuteursDeCommits(t *testing.T) {
	state := fakegh.NewState()
	state.AddRepo("acme", "projet-alpha", true)
	state.AddRepo("acme", "projet-beta", true)
	state.AddContributors("acme/projet-alpha", "emilie-cote", "jlpicard")
	state.AddContributors("acme/projet-beta", "aminata-d")
	h := nouveau(t, state)

	vue := h.apercuEquipe(map[string]any{"prefix": "projet", "scope": "a26.5n6.01"})
	for _, equipe := range vue.Teams {
		if equipe.Short == "alpha" {
			if strings.Join(equipe.Members, ",") != "emilie-cote,jlpicard" {
				t.Fatalf("membres d'alpha = %v", equipe.Members)
			}
			if equipe.Sources["emilie-cote"] != "commits" {
				t.Fatalf("sources d'alpha = %v", equipe.Sources)
			}
		}
	}
	if len(vue.Silent) != 0 {
		t.Fatalf("les commits suffisent à peupler les équipes : %v", vue.Silent)
	}
}

// Ce qu'on tranche à l'écran tient : le serveur ne redevine plus rien pour
// cette équipe, y compris quand on choisit de n'y mettre personne.
func TestRepriseEnEquipeRetientLaCompositionChoisie(t *testing.T) {
	h := nouveau(t, equipeOrg())
	vue := h.apercuEquipe(map[string]any{
		"prefix": "projet", "scope": "a26.5n6.01",
		"crews": map[string][]string{"alpha": {"aminata-d"}},
	})
	for _, equipe := range vue.Teams {
		if equipe.Short != "alpha" {
			continue
		}
		if strings.Join(equipe.Members, ",") != "aminata-d" {
			t.Fatalf("la composition choisie devrait tenir : %v", equipe.Members)
		}
		if equipe.Sources["aminata-d"] != "choisi" {
			t.Fatalf("sources d'alpha = %v", equipe.Sources)
		}
	}

	// Et jusqu'à l'écriture.
	h.travail(http.MethodPost, "/api/orgs/acme/import", map[string]any{
		"prefix": "projet", "scope": "a26.5n6.01", "teams": true,
		"crews": map[string][]string{"alpha": {"aminata-d"}},
	})
	membres := h.State.TeamMembers("acme", fakegh.TeamSlug("a26.5n6.01.alpha"))
	if strings.Join(membres, ",") != "aminata-d" {
		t.Fatalf("membres d'alpha sur GitHub = %v", membres)
	}
}

// Une équipe qu'on vide à la main reste vide : c'est une décision, pas un oubli.
func TestRepriseEnEquipePeutViderUneEquipe(t *testing.T) {
	h := nouveau(t, equipeOrg())
	vue := h.apercuEquipe(map[string]any{
		"prefix": "projet", "scope": "a26.5n6.01",
		"crews": map[string][]string{"alpha": {}},
	})
	for _, equipe := range vue.Teams {
		if equipe.Short == "alpha" && len(equipe.Members) != 0 {
			t.Fatalf("alpha devrait être vide : %v", equipe.Members)
		}
	}
	if len(vue.Silent) != 1 || vue.Silent[0] != "projet-alpha" {
		t.Fatalf("une équipe vide doit être signalée : %v", vue.Silent)
	}
}

// Une personne n'est que d'une équipe à la fois : une composition tranchée à
// l'écran ne doit pas être défaite par celle que l'équipe suivante devine.
func TestRepriseEnEquipeNeMetPersonneDansDeuxEquipes(t *testing.T) {
	state := fakegh.NewState()
	state.AddRepo("acme", "projet-alpha", true)
	state.AddRepo("acme", "projet-beta", true)
	// Aminata a poussé dans les deux : rien ne dit d'elle-même de quelle
	// équipe elle est.
	state.AddContributors("acme/projet-alpha", "aminata-d")
	state.AddContributors("acme/projet-beta", "aminata-d", "jlpicard")
	h := nouveau(t, state)

	vue := h.apercuEquipe(map[string]any{
		"prefix": "projet", "scope": "a26.5n6.01",
		"crews": map[string][]string{"beta": {"aminata-d", "jlpicard"}},
	})
	trouve := map[string][]string{}
	for _, equipe := range vue.Teams {
		trouve[equipe.Short] = equipe.Members
	}
	if strings.Join(trouve["beta"], ",") != "aminata-d,jlpicard" {
		t.Fatalf("la composition choisie devrait tenir : %v", trouve["beta"])
	}
	if len(trouve["alpha"]) != 0 {
		t.Fatalf("alpha ne devrait pas la reprendre : %v", trouve["alpha"])
	}
}

// ------------------------------------------------ nommer les membres

// Les équipes disent qui a fait le travail ; c'est la liste qui dit comment ces
// gens s'appellent. Sans ce rapprochement, le groupe n'aurait que des comptes.
func TestRepriseEnEquipeNommeLesMembres(t *testing.T) {
	state := classroomEquipes()
	h := nouveau(t, state)
	// Les profils GitHub portent les noms : c'est l'indice qui rapproche.
	state.Users["emilie-cote"] = "Émilie Côté"
	state.Users["jlpicard"] = "Jean-Luc Picard"

	vue := h.apercuEquipe(map[string]any{
		"prefix": "backend", "scope": "a25.5w5.1010",
		"content": []byte("nom_complet;no étudiant\nÉmilie Côté;1600105\n" +
			"Jean-Luc Picard;2100106\nÉdouard Loiselle;1900107\n"),
	})
	noms := map[string]string{}
	for _, personne := range vue.Students {
		noms[personne.Username] = personne.FullName
	}
	if noms["emilie-cote"] != "Émilie Côté" || noms["jlpicard"] != "Jean-Luc Picard" {
		t.Fatalf("les membres devraient être nommés : %+v", vue.Students)
	}
	// Une personne de la liste qu'aucune équipe ne réclame est signalée.
	if len(vue.Absent) != 1 || vue.Absent[0] != "Édouard Loiselle" {
		t.Fatalf("Édouard n'est dans aucune équipe : %v", vue.Absent)
	}
	// Et le rapprochement se montre, compte par compte.
	if len(vue.Pairings) == 0 {
		t.Fatalf("le rapprochement devrait être rendu : %+v", vue)
	}
}

// Un compte qu'aucun nom ne désigne rejoint quand même le groupe : son équipe
// l'y a mis, et le taire le ferait disparaître.
func TestRepriseEnEquipeInscritUnCompteSansNom(t *testing.T) {
	h := nouveau(t, classroomEquipes())
	vue := h.apercuEquipe(map[string]any{
		"prefix": "backend", "scope": "a25.5w5.1010",
		"content": []byte("nom_complet;no étudiant\nPersonne Inconnue;1111111\n"),
	})
	comptes := map[string]bool{}
	for _, personne := range vue.Students {
		comptes[personne.Username] = true
	}
	for _, compte := range []string{"emilie-cote", "jlpicard", "aminata-d", "wasim"} {
		if !comptes[compte] {
			t.Fatalf("@%s devrait rejoindre le groupe : %+v", compte, vue.Students)
		}
	}
	if len(vue.Unmatched) != 4 {
		t.Fatalf("les quatre comptes sont sans nom : %v", vue.Unmatched)
	}
}

// Le nom corrigé à l'écran a le dernier mot, et monte au registre.
func TestRepriseEnEquipeRetientLeNomCorrige(t *testing.T) {
	h := nouveau(t, classroomEquipes())
	h.travail(http.MethodPost, "/api/orgs/acme/import", map[string]any{
		"prefix": "backend", "scope": "a25.5w5.1010", "teams": true,
		"people": []map[string]string{
			{"full_name": "Émilie Côté", "username": "emilie-cote"},
		},
	})
	var liste struct {
		Students []struct {
			FullName string `json:"full_name"`
			Username string `json:"username"`
		} `json:"students"`
	}
	h.json(http.MethodGet, "/api/classrooms/a25.5w5.1010", nil, &liste)
	for _, personne := range liste.Students {
		if personne.Username == "emilie-cote" && personne.FullName != "Émilie Côté" {
			t.Fatalf("le nom corrigé devrait tenir : %+v", personne)
		}
	}
}
