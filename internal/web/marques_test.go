package web_test

import (
	"net/http"
	"sort"
	"testing"

	"github.com/PierreOlivierBrillant/gh-milou/internal/fakegh"
	"github.com/PierreOlivierBrillant/gh-milou/internal/signature"
)

// Chaque chemin du navigateur qui signe un dépôt verse sa marque au registre de
// l'organisation : c'est le seul endroit qui dira un jour à qui elle
// appartient, et le jeton, tiré au hasard, ne se retrouve plus ensuite.

// marqueDuDepot rend la marque déposée dans le README d'un dépôt.
func (h *harnais) marqueDuDepot(depot string) string {
	h.t.Helper()
	readme := h.State.Files("acme/"+depot, "main")[signature.ReadmeFile]
	token, signe := signature.First([]byte(readme))
	if !signe {
		h.t.Fatalf("« %s » n'est pas signé : %q", depot, readme)
	}
	return signature.Text(token)
}

// Ajouter une personne en cours de session lui remet les travaux cochés, signés
// comme ceux des autres. Leurs marques ne montaient jamais au registre : le
// dépôt était signé, mais rien ne disait à qui.
func TestAjouterUnePersonneVerseLesMarquesDeSesDepots(t *testing.T) {
	state := fakegh.NewState()
	state.Users["nchery"] = "Naomi Chéry"
	state.AddRepo("acme", "a26.5n6.01.tp1.jean-luc-picard", true)
	h := nouveau(t, state)
	place := h.groupe("a26", "5n6", "01", "Jean-Luc Picard", "jlpicard")

	bilan := h.travail(http.MethodPost, "/api/classrooms/"+place+"/students/add",
		map[string]any{
			"full_name": "Naomi Chéry", "username": "nchery",
			"assignments": []string{"tp1"},
		})
	if bilan["status"] != "terminé" {
		t.Fatalf("ajout %v : %v", bilan["status"], bilan["failure"])
	}

	marques := h.marquesAuRegistre()
	attendue := ligneDeMarque{
		Assignment: "a26.5n6.01.tp1", Username: "nchery",
		Token: h.marqueDuDepot("a26.5n6.01.tp1.naomi-chery"),
	}
	if len(marques) != 1 || marques[0] != attendue {
		t.Fatalf("registre des marques : %+v, attendu %+v", marques, attendue)
	}
}

// Un dépôt d'équipe reçoit sa marque comme celui d'une personne, et elle monte
// sous le nom de l'équipe. Ni compte ni matricule ne désigne une équipe : le
// registre n'avait pas de place pour elle, et la marque restait au dépôt.
func TestLaMarqueDUnDepotDEquipeMonteAuRegistre(t *testing.T) {
	h, place := groupeAvecEquipes(t)
	bilan := h.travail(http.MethodPost, "/api/classrooms/"+place+"/assignments",
		map[string]any{"name": "projet", "teams": true})
	if bilan["status"] != "terminé" {
		t.Fatalf("distribution en échec : %v", bilan)
	}

	marques := h.marquesAuRegistre()
	sort.Slice(marques, func(i, j int) bool { return marques[i].Team < marques[j].Team })
	attendues := []ligneDeMarque{
		{Assignment: "a26.5n6.01.projet", Team: "eq1",
			Token: h.marqueDuDepot("a26.5n6.01.projet.eq1")},
		{Assignment: "a26.5n6.01.projet", Team: "eq2",
			Token: h.marqueDuDepot("a26.5n6.01.projet.eq2")},
	}
	if len(marques) != 2 || marques[0] != attendues[0] || marques[1] != attendues[1] {
		t.Fatalf("registre des marques : %+v, attendu %+v", marques, attendues)
	}
}
