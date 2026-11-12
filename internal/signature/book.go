package signature

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/PierreOlivierBrillant/gh-milou/internal/naming"
	"github.com/PierreOlivierBrillant/gh-milou/internal/valid"
)

// Le registre des marques délivrées.
//
// Une marque est un jeton tiré au hasard : elle ne dit rien d'elle-même. Ce qui
// la relie à quelqu'un est ce fichier, et il vit dans le dépôt privé de
// l'organisation — là où vivent déjà les noms. L'étudiant n'y a jamais eu accès,
// et c'est ce qui fait qu'il ne peut ni lire sa marque ni deviner celle d'un
// autre.
//
// Deux travaux qui portent la même marque se reconnaissent sans ce fichier :
// la comparaison des jetons suffit. Il ne sert qu'à répondre à la question
// suivante — de qui s'agit-il —, et seul celui qui a distribué le travail peut
// y répondre. C'est voulu : un collègue qui reçoit des copies anonymisées voit
// la coïncidence sans jamais voir les noms.

// Version est celle du schéma écrit.
//
// Une marque délivrée à qui n'a pas encore de compte ne la change pas. Elle
// s'écrit sous son matricule, dans une ligne qu'un poste resté à une version
// antérieure de l'outil écarte en le signalant — il ne connaît que les lignes
// rangées sous un compte — tout en gardant les autres. Une version 2 lui ferait
// écarter le fichier entier, puisque c'est ce qu'il fait d'une version qu'il ne
// connaît pas, et sa prochaine distribution le réécrirait avec ses seules
// marques : perdre une marque est un désagrément, perdre toutes les autres
// serait une perte sèche.
const Version = 1

// BookFile porte les marques, dans le registre de l'organisation.
const BookFile = "signatures.json"

// Issued relie une marque à une personne, pour un travail.
type Issued struct {
	// Assignment est l'identifiant complet du travail.
	Assignment string `json:"assignment"`
	// Username est le compte de la personne. Vide, c'est son matricule qui la
	// désigne : une cohorte importée du collège n'a que lui, et sa marque ne
	// peut pas attendre qu'on lui rattache un compte — tirée au hasard, elle
	// serait perdue pour de bon.
	Username string `json:"username,omitempty"`
	// StudentID est le matricule de la personne, quand c'est lui qui la
	// désigne. La ligne n'est pas réécrite quand un compte lui est rattaché
	// ensuite : c'est le registre des utilisateurs qui relie l'un à l'autre.
	StudentID string `json:"student_id,omitempty"`
	// Token est la marque, sous sa forme lisible.
	Token    string `json:"token"`
	IssuedAt string `json:"issued_at,omitempty"`
}

// Key sert au rangement : un travail, une personne.
func (i Issued) Key() string {
	return strings.ToLower(i.Assignment + "\x00" + i.designation())
}

// designation dit ce qui désigne la personne, comme une liste de classe le
// fait : son compte, et à défaut son matricule. Celui-ci porte un « # »
// qu'aucun compte GitHub ne peut porter, si bien qu'un compte fait de chiffres
// et un matricule écrit pareil ne se confondent jamais.
func (i Issued) designation() string {
	if compte := strings.TrimSpace(i.Username); compte != "" {
		return compte
	}
	if matricule := strings.TrimSpace(i.StudentID); matricule != "" {
		return "#" + matricule
	}
	return ""
}

func (i Issued) validate() (Issued, error) {
	i.Assignment = strings.ToLower(strings.TrimSpace(i.Assignment))
	i.Username = strings.ToLower(strings.TrimSpace(i.Username))
	i.StudentID = strings.TrimSpace(i.StudentID)
	i.Token = strings.ToLower(strings.TrimSpace(i.Token))
	if _, _, ok := naming.SplitAssignment(i.Assignment); !ok {
		return i, valid.Errorf(
			"Signatures : « %s » n'est pas un travail de la nomenclature.", i.Assignment)
	}
	if i.designation() == "" {
		return i, valid.Errorf(
			"Signatures : une marque de « %s » ne dit pas à qui elle est.", i.Assignment)
	}
	if _, err := Parse(i.Token); err != nil {
		return i, err
	}
	return i, nil
}

// Book est le registre des marques délivrées.
type Book struct {
	Version int      `json:"version"`
	Issued  []Issued `json:"issued"`
}

// Empty dit qu'il n'y a rien à écrire.
func (b Book) Empty() bool { return len(b.Issued) == 0 }

// Validate met le registre en forme.
func (b Book) Validate() (Book, error) {
	b.Version = Version
	vues := map[string]int{}
	marques := map[string]string{}
	lignes := make([]Issued, 0, len(b.Issued))
	for _, ligne := range b.Issued {
		valide, err := ligne.validate()
		if err != nil {
			return b, err
		}
		// Deux personnes ne peuvent pas porter la même marque : la détection
		// les confondrait, et c'est exactement ce qu'elle existe pour éviter.
		if autre, deja := marques[valide.Token]; deja && autre != valide.Key() {
			return b, valid.Errorf(
				"Signatures : la marque « %s » est délivrée deux fois. Une marque "+
					"ne désigne qu'une personne.", valide.Token)
		}
		marques[valide.Token] = valide.Key()
		// Une même personne redistribuée reçoit une nouvelle marque : la plus
		// récente remplace l'ancienne. « Même » s'entend de ce qui la désigne :
		// une marque délivrée par matricule reste à côté de celle que reçoit
		// ensuite le compte rattaché, faute de rien dans ce fichier qui les
		// relie — et le registre des utilisateurs les attribue toutes deux à
		// la même personne.
		if rang, deja := vues[valide.Key()]; deja {
			lignes[rang] = valide
			continue
		}
		vues[valide.Key()] = len(lignes)
		lignes = append(lignes, valide)
	}
	sort.Slice(lignes, func(first, second int) bool {
		return lignes[first].Key() < lignes[second].Key()
	})
	b.Issued = lignes
	return b, nil
}

// With verse des marques et rend le registre qui en résulte, avec un booléen
// qui dit s'il a bougé.
func (b Book) With(issued []Issued) (Book, bool, error) {
	if len(issued) == 0 {
		return b, false, nil
	}
	fusion := Book{Version: Version, Issued: append([]Issued(nil), b.Issued...)}
	fusion.Issued = append(fusion.Issued, issued...)
	valide, err := fusion.Validate()
	if err != nil {
		return b, false, err
	}
	avant, err := EncodeBook(b)
	if err != nil {
		return b, false, err
	}
	apres, err := EncodeBook(valide)
	if err != nil {
		return b, false, err
	}
	return valide, string(avant) != string(apres), nil
}

// Who rend la personne derrière une marque, et le travail où elle a été posée.
func (b Book) Who(token string) (Issued, bool) {
	token = strings.ToLower(strings.TrimSpace(token))
	for _, ligne := range b.Issued {
		if ligne.Token == token {
			return ligne, true
		}
	}
	return Issued{}, false
}

// Of rend la marque d'une personne pour un travail. La personne se désigne
// comme dans une liste de classe : par son compte d'abord, par son matricule à
// défaut.
func (b Book) Of(assignment, handle string) (Issued, bool) {
	if strings.TrimSpace(handle) == "" {
		return Issued{}, false
	}
	for _, designee := range []Issued{
		{Assignment: assignment, Username: handle},
		{Assignment: assignment, StudentID: handle},
	} {
		cle := designee.Key()
		for _, ligne := range b.Issued {
			if ligne.Key() == cle {
				return ligne, true
			}
		}
	}
	return Issued{}, false
}

// DecodeBook relit un registre de marques.
func DecodeBook(content []byte) (Book, []string) {
	var lu Book
	if err := json.Unmarshal(content, &lu); err != nil {
		return Book{}, []string{fmt.Sprintf("Signatures illisibles : %v.", err)}
	}
	if lu.Version > Version {
		return Book{}, []string{fmt.Sprintf(
			"Signatures : elles viennent d'une version %d de l'outil, qui n'en "+
				"connaît que %d. Mettez l'extension à jour.", lu.Version, Version)}
	}
	// Une ligne mal écrite est écartée et signalée : perdre une marque est un
	// désagrément, perdre toutes les autres serait une perte sèche.
	gardes := make([]Issued, 0, len(lu.Issued))
	soucis := make([]string, 0, 2)
	for _, ligne := range lu.Issued {
		valide, err := ligne.validate()
		if err != nil {
			soucis = append(soucis, err.Error())
			continue
		}
		gardes = append(gardes, valide)
	}
	lu.Issued = gardes
	valide, err := lu.Validate()
	if err != nil {
		return Book{}, append(soucis, err.Error())
	}
	return valide, soucis
}

// EncodeBook écrit un registre de marques.
func EncodeBook(book Book) ([]byte, error) {
	valide, err := book.Validate()
	if err != nil {
		return nil, err
	}
	payload, err := json.MarshalIndent(valide, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(payload, '\n'), nil
}
