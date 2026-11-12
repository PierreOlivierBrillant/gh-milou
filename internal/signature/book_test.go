package signature_test

import (
	"strings"
	"testing"

	"github.com/PierreOlivierBrillant/gh-milou/internal/signature"
)

func marque(t *testing.T) string {
	t.Helper()
	token, err := signature.New()
	if err != nil {
		t.Fatalf("tirage : %v", err)
	}
	return signature.Text(token)
}

func registre(t *testing.T) (signature.Book, string, string) {
	t.Helper()
	premiere, seconde := marque(t), marque(t)
	book, err := signature.Book{Issued: []signature.Issued{
		{Assignment: "a26.5n6.01.tp1", Username: "emilie-cote", Token: premiere},
		{Assignment: "a26.5n6.01.tp1", Username: "jlpicard", Token: seconde},
	}}.Validate()
	if err != nil {
		t.Fatalf("registre refusé : %v", err)
	}
	return book, premiere, seconde
}

// Le registre est le seul endroit qui relie une marque à quelqu'un : deux
// travaux qui portent la même se reconnaissent sans lui, mais nul autre ne peut
// dire de qui il s'agit.
func TestLeRegistreRepondADeQuiEstUneMarque(t *testing.T) {
	book, premiere, _ := registre(t)

	ligne, trouve := book.Who(premiere)
	if !trouve || ligne.Username != "emilie-cote" {
		t.Fatalf("recherche par marque : %+v", ligne)
	}
	if _, trouve := book.Of("a26.5n6.01.tp1", "jlpicard"); !trouve {
		t.Fatal("recherche par personne")
	}
	if _, trouve := book.Who(marque(t)); trouve {
		t.Fatal("une marque jamais délivrée ne doit désigner personne")
	}
}

// Deux personnes ne peuvent pas porter la même marque : la détection les
// confondrait, et c'est exactement ce qu'elle existe pour éviter.
func TestDeuxPersonnesNePeuventPasPorterLaMemeMarque(t *testing.T) {
	commune := marque(t)
	_, err := signature.Book{Issued: []signature.Issued{
		{Assignment: "a26.5n6.01.tp1", Username: "emilie-cote", Token: commune},
		{Assignment: "a26.5n6.01.tp1", Username: "jlpicard", Token: commune},
	}}.Validate()
	if err == nil || !strings.Contains(err.Error(), commune) {
		t.Fatalf("le doublon doit être refusé en nommant la marque : %v", err)
	}
}

// Redistribuer un travail délivre une nouvelle marque : c'est la plus récente
// qui vaut, et l'ancienne ne doit pas rester à désigner la même personne.
func TestRedistribuerRemplaceLaMarque(t *testing.T) {
	book, premiere, _ := registre(t)
	nouvelle := marque(t)

	apres, bouge, err := book.With([]signature.Issued{
		{Assignment: "a26.5n6.01.tp1", Username: "emilie-cote", Token: nouvelle},
	})
	if err != nil || !bouge {
		t.Fatalf("versement : %v", err)
	}
	if len(apres.Issued) != 2 {
		t.Fatalf("lignes : %+v", apres.Issued)
	}
	if ligne, _ := apres.Of("a26.5n6.01.tp1", "emilie-cote"); ligne.Token != nouvelle {
		t.Fatalf("la marque la plus récente doit gagner : %+v", ligne)
	}
	if _, trouve := apres.Who(premiere); trouve {
		t.Fatal("l'ancienne marque ne doit plus désigner personne")
	}
	// Celle du voisin survit : verser les siennes ne retire pas les autres.
	if _, trouve := apres.Of("a26.5n6.01.tp1", "jlpicard"); !trouve {
		t.Fatal("la marque d'un autre a disparu")
	}
	// Et verser ce qui s'y trouve déjà ne bouge rien.
	if _, bouge, _ := apres.With(apres.Issued); bouge {
		t.Fatal("un versement sans changement ne doit rien écrire")
	}
}

func TestDesMarquesIncompletesSontRefusees(t *testing.T) {
	bonne := marque(t)
	cas := map[string]signature.Issued{
		"travail hors nomenclature": {Assignment: "tp1", Username: "x", Token: bonne},
		"ni compte ni matricule":    {Assignment: "a26.5n6.01.tp1", Token: bonne},
		"marque invalide": {Assignment: "a26.5n6.01.tp1", Username: "x",
			Token: "pas-une-marque"},
	}
	for nom, ligne := range cas {
		if _, err := (signature.Book{Issued: []signature.Issued{ligne}}).Validate(); err == nil {
			t.Fatalf("« %s » aurait dû être refusé", nom)
		}
	}
}

// Le fichier se modifie à la main sur github.com : perdre une marque est un
// désagrément, perdre toutes les autres serait une perte sèche.
func TestUneLigneMalEcriteNEmportePasLeRegistre(t *testing.T) {
	book, _, _ := registre(t)
	contenu, err := signature.EncodeBook(book)
	if err != nil {
		t.Fatalf("écriture : %v", err)
	}
	abime := strings.Replace(string(contenu), `"a26.5n6.01.tp1"`, `"tp1"`, 1)

	relu, soucis := signature.DecodeBook([]byte(abime))
	if len(relu.Issued) != 1 {
		t.Fatalf("lignes gardées : %+v", relu.Issued)
	}
	if len(soucis) != 1 {
		t.Fatalf("la ligne fautive doit être signalée : %v", soucis)
	}
	if _, soucis := signature.DecodeBook([]byte(`{"version": 99}`)); len(soucis) == 0 ||
		!strings.Contains(soucis[0], "Mettez l'extension à jour") {
		t.Fatalf("une version future doit proposer la mise à jour : %v", soucis)
	}
}

// Une inscrite sans compte — toute cohorte importée du collège — reçoit sa
// marque comme les autres. Elle se range sous son matricule : c'est ce qui la
// désigne tant qu'aucun compte ne lui est rattaché, et la marque, tirée au
// hasard, ne peut pas attendre ce compte sans être perdue.
func TestUneMarqueSeDelivreAQuiNAQueSonMatricule(t *testing.T) {
	book, _, _ := registre(t)
	sienne := marque(t)

	apres, bouge, err := book.With([]signature.Issued{
		{Assignment: "a26.5n6.01.tp1", StudentID: "2100456", Token: sienne},
	})
	if err != nil || !bouge {
		t.Fatalf("une marque sans compte doit se verser : %v", err)
	}
	ligne, trouve := apres.Who(sienne)
	if !trouve || ligne.StudentID != "2100456" || ligne.Username != "" {
		t.Fatalf("recherche par marque : %+v", ligne)
	}
	if ligne, trouve := apres.Of("a26.5n6.01.tp1", "2100456"); !trouve || ligne.Token != sienne {
		t.Fatalf("recherche par matricule : %+v", ligne)
	}

	// Redistribuer lui délivre une nouvelle marque, qui remplace l'ancienne :
	// la règle est celle d'un compte.
	nouvelle := marque(t)
	apres, _, err = apres.With([]signature.Issued{
		{Assignment: "a26.5n6.01.tp1", StudentID: "2100456", Token: nouvelle},
	})
	if err != nil || len(apres.Issued) != 3 {
		t.Fatalf("redistribution : %v, %+v", err, apres.Issued)
	}
	if _, trouve := apres.Who(sienne); trouve {
		t.Fatal("l'ancienne marque ne doit plus désigner personne")
	}
}

// Un compte et un matricule ne se confondent jamais, même écrits pareil : un
// compte GitHub peut n'être fait que de chiffres.
func TestUnCompteEtUnMatriculeNeSeConfondentPas(t *testing.T) {
	duCompte, duMatricule := marque(t), marque(t)
	book, err := signature.Book{Issued: []signature.Issued{
		{Assignment: "a26.5n6.01.tp1", Username: "2100456", Token: duCompte},
		{Assignment: "a26.5n6.01.tp1", StudentID: "2100456", Token: duMatricule},
	}}.Validate()
	if err != nil || len(book.Issued) != 2 {
		t.Fatalf("deux personnes, deux marques : %v, %+v", err, book.Issued)
	}
	// Le compte passe d'abord, comme partout où l'on désigne quelqu'un.
	if ligne, _ := book.Of("a26.5n6.01.tp1", "2100456"); ligne.Token != duCompte {
		t.Fatalf("le compte doit l'emporter sur le matricule : %+v", ligne)
	}
}

// Un registre déjà écrit se relit, et se réécrit à l'identique : ses lignes ne
// changent pas de forme. Une marque sans compte ne change pas non plus la
// version du fichier — un poste resté à la précédente écarte cette ligne en le
// signalant, et garde toutes les autres.
func TestUnRegistreDejaEcritSeRelitEtSeReecritALIdentique(t *testing.T) {
	premiere, seconde := marque(t), marque(t)
	ecrit := `{
  "version": 1,
  "issued": [
    {
      "assignment": "a26.5n6.01.tp1",
      "username": "emilie-cote",
      "token": "` + premiere + `",
      "issued_at": "2026-09-01T10:00:00Z"
    },
    {
      "assignment": "a26.5n6.01.tp1",
      "username": "jlpicard",
      "token": "` + seconde + `"
    }
  ]
}
`
	relu, soucis := signature.DecodeBook([]byte(ecrit))
	if len(soucis) != 0 || len(relu.Issued) != 2 {
		t.Fatalf("relecture : %v, %+v", soucis, relu.Issued)
	}
	reecrit, err := signature.EncodeBook(relu)
	if err != nil || string(reecrit) != ecrit {
		t.Fatalf("réécriture (%v) :\n%s\nattendu :\n%s", err, reecrit, ecrit)
	}

	sienne := marque(t)
	avecMatricule, _, err := relu.With([]signature.Issued{
		{Assignment: "a26.5n6.01.tp1", StudentID: "2100456", Token: sienne},
	})
	if err != nil {
		t.Fatalf("versement : %v", err)
	}
	contenu, err := signature.EncodeBook(avecMatricule)
	if err != nil {
		t.Fatalf("écriture : %v", err)
	}
	texte := string(contenu)
	if !strings.Contains(texte, `"version": 1`) ||
		!strings.Contains(texte, `"student_id": "2100456"`) ||
		strings.Contains(texte, `"username": ""`) {
		t.Fatalf("une ligne sans compte s'écrit sous son matricule, en version 1 :\n%s", texte)
	}
	if relu, soucis := signature.DecodeBook(contenu); len(soucis) != 0 ||
		len(relu.Issued) != 3 {
		t.Fatalf("relecture : %v, %+v", soucis, relu.Issued)
	}
}
