package web

import (
	"strings"
	"testing"

	"github.com/PierreOlivierBrillant/gh-milou/internal/brand"
)

// Le terminal lit ses couleurs dans brand, le navigateur dans theme.css. Rien
// ne les relie à l'exécution : c'est ce test qui les tient ensemble, pour
// qu'une teinte changée d'un côté ne laisse pas l'autre en arrière.
func TestLaPaletteDuWebEstCelleDeLaMarque(t *testing.T) {
	feuille, err := assets.ReadFile("assets/theme.css")
	if err != nil {
		t.Fatal(err)
	}
	css := strings.ToLower(string(feuille))
	for nom, couleur := range map[string]string{
		"Encre": brand.Encre, "Papier": brand.Papier, "Bleu": brand.Bleu,
		"BleuClair": brand.BleuClair, "Vert": brand.Vert, "Rouge": brand.Rouge,
		"Jaune": brand.Jaune, "Gris": brand.Gris,
	} {
		if !strings.Contains(css, strings.ToLower(couleur)) {
			t.Errorf("brand.%s = %s n'apparaît pas dans theme.css : le terminal et le navigateur divergent", nom, couleur)
		}
	}
}

// La page reçoit la commande dans « /api/context » : écrite en dur, elle
// survivrait au prochain renommage de l'outil, comme « gh milou » avait
// survécu, dans trois notes, au passage de Nestor à Milou. Les commentaires
// peuvent la citer ; ce que la page affiche, non.
func TestLaPageNEcritPasLaCommandeEnDur(t *testing.T) {
	commande := strings.ToLower(brand.Command)
	for _, fichier := range []string{"assets/app.js", "assets/index.html"} {
		contenu, err := assets.ReadFile(fichier)
		if err != nil {
			t.Fatal(err)
		}
		for numero, ligne := range strings.Split(string(contenu), "\n") {
			nue := strings.TrimSpace(ligne)
			if strings.HasPrefix(nue, "//") || strings.HasPrefix(nue, "*") ||
				strings.HasPrefix(nue, "/*") || strings.HasPrefix(nue, "<!--") {
				continue
			}
			if strings.Contains(strings.ToLower(ligne), commande) {
				t.Errorf("%s:%d écrit « %s » en dur : %s", fichier, numero+1,
					brand.Command, nue)
			}
		}
	}
}

func TestLaPageChargeSonThemeAvantSesComposants(t *testing.T) {
	page, err := assets.ReadFile("assets/index.html")
	if err != nil {
		t.Fatal(err)
	}
	html := string(page)
	theme, composants := strings.Index(html, "/theme.css"), strings.Index(html, "/app.css")
	if theme < 0 || composants < 0 || theme > composants {
		t.Error("theme.css doit être lié avant app.css : les composants lisent ses jetons")
	}
	if !strings.Contains(html, "<title>"+brand.Name+"</title>") {
		t.Errorf("le titre de la page doit être « %s »", brand.Name)
	}
	if !strings.Contains(html, brand.Tagline) {
		t.Errorf("l'accueil doit porter la devise de la marque : « %s »", brand.Tagline)
	}
}

func TestLattenteDuDemarrageFaitCourirMilou(t *testing.T) {
	// Le démarrage est le seul écran qui bloque toute la page ; c'est là, et
	// là seulement, que Milou court. Les attentes locales gardent leur roue.
	page, err := assets.ReadFile("assets/index.html")
	if err != nil {
		t.Fatal(err)
	}
	html := string(page)
	demarrage := html[strings.Index(html, `id="vue-demarrage"`):]
	demarrage = demarrage[:strings.Index(demarrage, "</section>")]
	if !strings.Contains(demarrage, "milou-court") {
		t.Error("la vue de démarrage doit montrer Milou qui court")
	}
	if strings.Contains(html, "roue") {
		t.Error("la roue n'a plus sa place : c'est Milou qui attend")
	}
	if strings.Count(html, "milou-court") != 1 {
		t.Error("le grand Milou ne court que sur l'écran de démarrage")
	}
	if !strings.Contains(demarrage, "milou-confus") {
		t.Error("quand le serveur ne répond pas, c'est Milou étourdi qui le dit")
	}
}

func TestLesDessinsDeMilouSontEmbarques(t *testing.T) {
	// L'icône et les dessins sont servis par le binaire lui-même : une image
	// absente de l'embarquement serait un carré vide dans l'interface.
	for _, nom := range []string{
		"assets/favicon.png", "assets/milou/tete.png", "assets/milou/astronaute.png",
		"assets/milou/confus.png", "assets/milou/habille.png", "assets/milou/roi.png",
	} {
		contenu, err := assets.ReadFile(nom)
		if err != nil {
			t.Errorf("%s : %v", nom, err)
			continue
		}
		if !strings.HasPrefix(string(contenu), "\x89PNG") {
			t.Errorf("%s n'est pas un PNG", nom)
		}
	}
	page, err := assets.ReadFile("assets/index.html")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(page), `href="/favicon.png"`) {
		t.Error("l'icône de la page est le PNG de la tête de Milou")
	}
}

func TestLesComposantsNeCodentAucuneCouleur(t *testing.T) {
	// Une couleur écrite en dur dans app.css échappe au thème : elle ne suit
	// ni le mode sombre ni un changement de palette. Tout passe par un jeton.
	feuille, err := assets.ReadFile("assets/app.css")
	if err != nil {
		t.Fatal(err)
	}
	for numero, ligne := range strings.Split(string(feuille), "\n") {
		if strings.Contains(ligne, "#") && !strings.Contains(ligne, "/*") && !strings.HasPrefix(strings.TrimSpace(ligne), "#") {
			t.Errorf("app.css:%d : une couleur codée en dur — %s", numero+1, strings.TrimSpace(ligne))
		}
		if strings.Contains(ligne, "rgba(") || strings.Contains(ligne, "rgb(") {
			t.Errorf("app.css:%d : une couleur codée en dur — %s", numero+1, strings.TrimSpace(ligne))
		}
	}
}
