package roster_test

import (
	"testing"

	"github.com/PierreOlivierBrillant/gh-milou/internal/roster"
)

// omnivox écrit un export de Léa octet par octet : Windows-1252, séparateur
// « ; », champs blindés « ="…" » et fins de ligne CRLF. Jamais un vrai fichier
// dans le dépôt — il porterait des noms d'étudiants.
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

// Une liste du collège ne porte aucun compte GitHub, et ce n'est pas une faute.
//
// « People » ne retient que les personnes qui en ont un : devant un export
// d'Omnivox, il est vide, sans aucun rejet à signaler puisque rien n'est
// fautif. Toute façade qui s'y fiait annonçait donc une liste vide devant un
// fichier plein — et le disait parfois si mal qu'elle ne disait rien du tout.
func TestUneListeSansCompteSeLitQuandMeme(t *testing.T) {
	lue := roster.ParseBytes(omnivox(
		"Numéro d'étudiant;Nom de l'étudiant;Prénom;Code permanent",
		`="1600105";="Aube-Lachance";="Lorenzo";="AUBL01010101"`,
		`="2100123";="Côté";="Émilie";="COTE03030303"`,
	))

	if len(lue.Issues) != 0 {
		t.Fatalf("rien n'est fautif dans ce fichier : %v", lue.Issues)
	}
	if lue.Named() {
		t.Fatalf("cette liste ne porte aucun compte : %v", lue.People)
	}
	tous := lue.Everyone()
	if len(tous) != 2 {
		t.Fatalf("%d personne(s) lue(s), 2 attendues : %+v", len(tous), tous)
	}
	// Le prénom passe devant : c'est ainsi qu'on nomme quelqu'un, et ainsi que
	// son nom entrera dans celui de ses dépôts.
	if tous[0].FullName != "Lorenzo Aube-Lachance" || tous[0].StudentID != "1600105" {
		t.Errorf("première personne : %+v", tous[0])
	}
	if tous[1].FullName != "Émilie Côté" || tous[1].Username != "" {
		t.Errorf("seconde personne : %+v", tous[1])
	}
}
