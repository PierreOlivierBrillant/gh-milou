package web_test

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"testing"

	"github.com/PierreOlivierBrillant/gh-milou/internal/fakegh"
	"github.com/PierreOlivierBrillant/gh-milou/internal/registry"
	"github.com/PierreOlivierBrillant/gh-milou/internal/runner"
	"github.com/PierreOlivierBrillant/gh-milou/internal/signature"
)

// Inscrire une cohorte telle que le collège l'exporte.
//
// Un export de Léa ne porte aucun compte GitHub : un numéro d'étudiant, un nom,
// un code permanent. L'interface n'en retenait que les personnes déjà pourvues
// d'un compte — c'est-à-dire aucune — et refusait la liste entière : « Aucun
// étudiant dans la liste fournie », devant un fichier de vingt-trois noms. La
// lecture, elle, s'était bien passée ; c'est la façade qui jetait tout.

// omnivox écrit un export de Léa octet par octet : Windows-1252, séparateur
// « ; », champs blindés et fins de ligne CRLF. Jamais un vrai fichier dans le
// dépôt — il porterait des noms d'étudiants.
func omnivox(lignes ...string) []byte {
	var octets []byte
	for _, ligne := range lignes {
		for _, caractere := range ligne {
			if caractere < 0x100 {
				octets = append(octets, byte(caractere))
				continue
			}
			octets = append(octets, '?')
		}
		octets = append(octets, '\r', '\n')
	}
	return octets
}

var exportDeLea = omnivox(
	"Numéro d'étudiant;Nom de l'étudiant;Prénom;Code permanent",
	`="1600105";="Aube-Lachance";="Lorenzo";="AUBL01010101"`,
	`="1200101";="Rowlinson-Hayes";="Preston";="ROWP02020202"`,
	`="2100123";="Côté";="Émilie";="COTE03030303"`,
)

// La lecture rend les personnes, et le dit, même sans un seul compte.
func TestLaLectureDUnExportDeLeaRendLesPersonnes(t *testing.T) {
	h := nouveau(t, fakegh.NewState())

	var lue struct {
		People []struct {
			FullName  string `json:"full_name"`
			Username  string `json:"username"`
			StudentID string `json:"student_id"`
		} `json:"people"`
		Issues []any `json:"issues"`
	}
	h.json(http.MethodPost, "/api/roster/parse",
		map[string]any{"content": exportDeLea}, &lue)

	if len(lue.Issues) != 0 {
		t.Fatalf("rien n'est fautif dans cet export : %v", lue.Issues)
	}
	if len(lue.People) != 3 {
		t.Fatalf("%d personne(s) rendues, 3 attendues : %+v", len(lue.People), lue.People)
	}
	if lue.People[0].FullName != "Lorenzo Aube-Lachance" ||
		lue.People[0].StudentID != "1600105" || lue.People[0].Username != "" {
		t.Errorf("première personne : %+v", lue.People[0])
	}
}

// Et le groupe les garde : c'est le geste de septembre.
func TestUnExportDeLeaInscritLaCohorteEntiere(t *testing.T) {
	h := nouveau(t, fakegh.NewState())
	place := h.groupe("a26", "5n6", "1010", "Émilie Côté", "ecote")

	var lue struct {
		People []map[string]any `json:"people"`
	}
	h.json(http.MethodPost, "/api/roster/parse",
		map[string]any{"content": exportDeLea}, &lue)

	reponse, contenu := h.requete(http.MethodPost,
		"/api/classrooms/"+place+"/students", map[string]any{"people": lue.People})
	if reponse.StatusCode >= 300 {
		t.Fatalf("inscription refusée : %d — %s", reponse.StatusCode, contenu)
	}

	var liste struct {
		Students []struct {
			FullName string `json:"full_name"`
		} `json:"students"`
		Total int `json:"total"`
	}
	h.json(http.MethodGet, "/api/classrooms/"+place+"/students", nil, &liste)
	if liste.Total != 3 {
		t.Fatalf("%d étudiant(s) dans le groupe, 3 attendus : %+v",
			liste.Total, liste.Students)
	}
}

// Et la distribution suit : un dépôt par personne, nommé par son nom puisque
// c'est lui que la nomenclature porte — et le bilan dit sans détour que
// personne n'y a été invité, faute de compte. Une case vide se lirait comme un
// oubli d'affichage ; c'est un fait, et celui dont dépend l'accès.
func TestUnDepotEstCreePourQuiNAPasEncoreDeCompte(t *testing.T) {
	state := fakegh.NewState()
	state.Users["ecote"] = "Émilie Côté"
	state.Users["laubelachance"] = "Lorenzo Aube-Lachance"
	h := nouveau(t, state)
	place := h.groupe("a26", "5n6", "1010", "Émilie Côté", "ecote")

	var lue struct {
		People []map[string]any `json:"people"`
	}
	h.json(http.MethodPost, "/api/roster/parse",
		map[string]any{"content": exportDeLea}, &lue)
	h.json(http.MethodPost, "/api/classrooms/"+place+"/students",
		map[string]any{"people": lue.People}, nil)

	// Lorenzo a créé son compte depuis : on le lui rattache, et son matricule
	// est tout ce qu'on a pour le désigner.
	if reponse, contenu := h.rattacher(place, "1600105", "laubelachance"); reponse.StatusCode >= 300 {
		t.Fatalf("rattachement par matricule refusé : %d — %s", reponse.StatusCode, contenu)
	}

	rapport := h.travail(http.MethodPost, "/api/classrooms/"+place+"/assignments",
		map[string]any{"name": "tp1"})
	brut, err := json.Marshal(rapport)
	if err != nil {
		t.Fatalf("bilan illisible : %v", err)
	}
	bilan := string(brut)

	noms := h.depots()
	sort.Strings(noms)
	attendus := []string{
		"a26.5n6.1010.tp1.emilie-cote",
		"a26.5n6.1010.tp1.lorenzo-aube-lachance",
		"a26.5n6.1010.tp1.preston-rowlinson-hayes",
	}
	if strings.Join(noms, ",") != strings.Join(attendus, ",") {
		t.Fatalf("un dépôt par personne attendu :\n  %v\n  %v", noms, attendus)
	}
	// Lorenzo, à qui l'on vient de rattacher un compte, est invité sur le sien.
	invites := h.State.Invitations["acme/a26.5n6.1010.tp1.lorenzo-aube-lachance"]
	if len(invites) != 1 || !strings.EqualFold(invites[0].Login, "laubelachance") {
		t.Errorf("Lorenzo devrait être invité sous son compte : %+v", invites)
	}
	// Preston n'en a toujours pas : son dépôt existe, et personne n'y est
	// invité. C'est un fait, pas un échec.
	if invites := h.State.Invitations["acme/a26.5n6.1010.tp1.preston-rowlinson-hayes"]; len(invites) != 0 {
		t.Errorf("personne n'était à inviter : %+v", invites)
	}
	// Et le bilan le dit, plutôt que de laisser la case vide : une case vide se
	// lit comme un oubli d'affichage, alors que c'est le fait dont dépend
	// l'accès au dépôt.
	if !strings.Contains(bilan, runner.CollaboratorUnknown) {
		t.Errorf("le bilan ne dit pas que personne n'était à inviter : %s", bilan)
	}
}

// inscritParCompte et inscritParMatricule décrivent une ligne de liste de
// classe : l'une désignée par son compte, l'autre par son seul matricule.
func inscritParCompte(nom, compte string) map[string]any {
	return map[string]any{"full_name": nom, "username": compte}
}

func inscritParMatricule(nom, matricule string) map[string]any {
	return map[string]any{"full_name": nom, "student_id": matricule}
}

// poserListe remplace la liste d'un groupe, comme un import de fichier.
func (h *harnais) poserListe(place string, gens ...map[string]any) {
	h.t.Helper()
	h.json(http.MethodPost, "/api/classrooms/"+place+"/students",
		map[string]any{"people": gens}, nil)
}

// apercuDeDistribution rend les dépôts qu'une distribution créerait, et les
// noms de ceux qu'elle écarte parce qu'ils ont déjà le leur.
func (h *harnais) apercuDeDistribution(place string, corps map[string]any) (depots, servis []string) {
	h.t.Helper()
	var apercu struct {
		Items []struct {
			Name string `json:"name"`
		} `json:"items"`
		Served []struct {
			FullName string `json:"full_name"`
		} `json:"served"`
	}
	h.json(http.MethodPost, "/api/classrooms/"+place+"/assignments/preview", corps, &apercu)
	for _, item := range apercu.Items {
		depots = append(depots, item.Name)
	}
	for _, personne := range apercu.Served {
		servis = append(servis, personne.FullName)
	}
	sort.Strings(depots)
	return depots, servis
}

// Le navigateur envoie toujours ce qu'il a coché, tout le groupe compris. Qui
// n'a encore qu'un matricule y est désigné par lui : l'étape « Distribution »
// le montrait coché, puis l'oubliait — six étudiants cochés, cinq dépôts.
func TestLaSelectionDesigneParSonMatriculeQuiNAPasDeCompte(t *testing.T) {
	h := nouveau(t, fakegh.NewState())
	place := h.groupe("a26", "5n6", "1010", "Jean-Luc Picard", "jlpicard")
	h.poserListe(place, inscritParCompte("Jean-Luc Picard", "jlpicard"),
		inscritParMatricule("Nadia Pépin", "2100110"))

	depots, _ := h.apercuDeDistribution(place, map[string]any{
		"name": "tp1", "usernames": []string{"jlpicard", "2100110"},
	})
	attendus := []string{"a26.5n6.1010.tp1.jean-luc-picard", "a26.5n6.1010.tp1.nadia-pepin"}
	if strings.Join(depots, ",") != strings.Join(attendus, ",") {
		t.Fatalf("aperçu :\n  %v\nattendu :\n  %v", depots, attendus)
	}

	h.travail(http.MethodPost, "/api/classrooms/"+place+"/assignments",
		map[string]any{"name": "tp1", "usernames": []string{"2100110"}})
	if noms := h.depots(); len(noms) != 1 || noms[0] != "a26.5n6.1010.tp1.nadia-pepin" {
		t.Fatalf("dépôts créés : %v", noms)
	}
}

// Le dépôt d'une inscrite sans compte ne tient pas les autres pour servies. Il
// les y tenait toutes : elles n'ont aucun compte, et c'est ce compte absent qui
// servait de clé — si bien que « Distribuer aux manquants » en oubliait.
func TestLeDepotDUneInscriteSansCompteNeServPasLesAutres(t *testing.T) {
	state := fakegh.NewState()
	state.AddRepo("acme", "a26.5n6.1010.tp1.nadia-pepin", true)
	h := nouveau(t, state)
	place := h.groupe("a26", "5n6", "1010", "Jean-Luc Picard", "jlpicard")
	h.poserListe(place, inscritParCompte("Jean-Luc Picard", "jlpicard"),
		inscritParMatricule("Nadia Pépin", "2100110"),
		inscritParMatricule("Aminata Diallo", "2100789"))

	depots, servis := h.apercuDeDistribution(place, map[string]any{"name": "tp1"})
	attendus := []string{"a26.5n6.1010.tp1.aminata-diallo", "a26.5n6.1010.tp1.jean-luc-picard"}
	if strings.Join(depots, ",") != strings.Join(attendus, ",") {
		t.Errorf("aperçu :\n  %v\nattendu :\n  %v", depots, attendus)
	}
	if strings.Join(servis, ",") != "Nadia Pépin" {
		t.Errorf("seule Nadia a déjà son dépôt : %v", servis)
	}
}

// ligneDeMarque est une ligne du registre des marques, telle qu'elle y est
// écrite : c'est ce fichier, et lui seul, qui dira un jour à qui une marque
// appartient.
type ligneDeMarque struct {
	Assignment string `json:"assignment"`
	Username   string `json:"username"`
	StudentID  string `json:"student_id"`
	Team       string `json:"team"`
	Token      string `json:"token"`
}

// marquesAuRegistre relit les marques que porte le registre de l'organisation.
func (h *harnais) marquesAuRegistre() []ligneDeMarque {
	h.t.Helper()
	contenu, present := h.State.Files("acme/"+registry.RepoName, registry.Branch)[registry.MarksFile]
	if !present {
		return nil
	}
	var lu struct {
		Issued []ligneDeMarque `json:"issued"`
	}
	if err := json.Unmarshal([]byte(contenu), &lu); err != nil {
		h.t.Fatalf("registre des marques illisible : %v\n%s", err, contenu)
	}
	return lu.Issued
}

// distribuerANadia distribue « tp1 » à un groupe où Nadia Pépin n'a encore que
// son matricule, et rend la marque déposée dans son dépôt.
func distribuerANadia(t *testing.T) (*harnais, string, string) {
	t.Helper()
	state := fakegh.NewState()
	state.Users["jlpicard"] = "Jean-Luc Picard"
	state.Users["npepin"] = "Nadia Pépin"
	h := nouveau(t, state)
	place := h.groupe("a26", "5n6", "1010", "Jean-Luc Picard", "jlpicard")
	h.poserListe(place, inscritParCompte("Jean-Luc Picard", "jlpicard"),
		inscritParMatricule("Nadia Pépin", "2100110"))

	h.travail(http.MethodPost, "/api/classrooms/"+place+"/assignments",
		map[string]any{"name": "tp1"})

	readme := h.State.Files("acme/a26.5n6.1010.tp1.nadia-pepin", "main")["README.md"]
	token, signe := signature.First([]byte(readme))
	if !signe {
		t.Fatalf("le dépôt de Nadia n'est pas signé : %q", readme)
	}
	return h, place, signature.Text(token)
}

// La marque d'une inscrite sans compte monte au registre, rattachée à son
// matricule. Elle restait dans son dépôt sans y monter : le jeton est tiré au
// hasard, et ce qui ne monte pas est perdu pour de bon — lui rattacher un
// compte ensuite n'y changeait rien.
func TestLaMarqueDUneInscriteSansCompteMonteAuRegistre(t *testing.T) {
	h, _, marque := distribuerANadia(t)

	marques := h.marquesAuRegistre()
	var sienne, siennes = ligneDeMarque{}, 0
	for _, ligne := range marques {
		if ligne.Token == marque {
			sienne, siennes = ligne, siennes+1
		}
	}
	if siennes != 1 {
		t.Fatalf("la marque de Nadia (%s) n'est pas au registre : %+v", marque, marques)
	}
	if sienne.StudentID != "2100110" || sienne.Username != "" ||
		sienne.Assignment != "a26.5n6.1010.tp1" {
		t.Errorf("sa marque doit être rattachée à son matricule : %+v", sienne)
	}
	// Rien ne change pour qui a un compte : sa marque est rangée sous lui.
	if len(marques) != 2 {
		t.Errorf("deux marques attendues, une par dépôt : %+v", marques)
	}
	for _, ligne := range marques {
		if ligne.Token != marque && (ligne.Username != "jlpicard" || ligne.StudentID != "") {
			t.Errorf("la marque de Jean-Luc doit rester sous son compte : %+v", ligne)
		}
	}
}

// Et elle reste attribuable : une copie qui la porte se nomme par son matricule
// tant que Nadia n'a pas de compte, puis par son nom dès qu'on lui en rattache
// un. Le registre apprend le matricule avec le compte : c'est ce qui les relie,
// et sans lui la marque ne désignerait plus qu'un matricule que rien ne nomme.
func TestSaMarqueResteAttribuableUneFoisSonCompteRattache(t *testing.T) {
	h, place, marque := distribuerANadia(t)

	// Jean-Luc a recopié le travail de Nadia, README compris : la marque a
	// suivi.
	readme := h.State.Files("acme/a26.5n6.1010.tp1.nadia-pepin", "main")["README.md"]
	for _, depot := range []string{"a26.5n6.1010.tp1.nadia-pepin", "a26.5n6.1010.tp1.jean-luc-picard"} {
		h.State.SeedCommit("acme/"+depot, map[string]string{
			"README.md": readme, "src/Solution.java": solutionJava,
		}, "main")
	}
	bilan := h.travail(http.MethodPost,
		"/api/classrooms/"+place+"/assignments/tp1/plagiat",
		map[string]any{"profile": "tout"})
	resultat, _ := bilan["result"].(map[string]any)
	nom, _ := resultat["report"].(string)
	if nom == "" {
		t.Fatalf("aucun rapport produit : %+v", bilan)
	}

	porteur := func() map[string]any {
		t.Helper()
		var rapport struct {
			Marks map[string]map[string]any `json:"marks"`
		}
		h.json(http.MethodGet, "/api/plagiat/reports/"+nom, nil, &rapport)
		return rapport.Marks[marque]
	}

	avant := porteur()
	if phrase, _ := avant["phrase"].(string); !strings.Contains(phrase, "matricule 2100110") ||
		!strings.Contains(phrase, "a26.5n6.1010.tp1") {
		t.Fatalf("sans compte, la marque se nomme par son matricule : %+v", avant)
	}

	if reponse, contenu := h.rattacher(place, "2100110", "npepin"); reponse.StatusCode >= 300 {
		t.Fatalf("rattachement refusé : %d — %s", reponse.StatusCode, contenu)
	}
	apres := porteur()
	if phrase, _ := apres["phrase"].(string); !strings.Contains(phrase, "Nadia Pépin") ||
		apres["username"] != "npepin" {
		t.Fatalf("une fois son compte rattaché, la marque la nomme : %+v", apres)
	}
}
