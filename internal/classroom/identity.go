package classroom

import (
	"sort"
	"strings"

	"github.com/PierreOlivierBrillant/gh-milou/internal/roster"
	"github.com/PierreOlivierBrillant/gh-milou/internal/valid"
)

// Une personne, plusieurs comptes.
//
// Un étudiant travaille parfois sous deux comptes : celui que GitHub Classroom
// lui a fait créer et le sien, celui du collège et celui d'avant. La liste du
// groupe peut le dire — « Also » porte les autres comptes d'une personne —, et
// tout ce qui suit en tient compte : elle n'a qu'un dépôt, puisque c'est son
// nom qui le nomme, et elle y est invitée sous chacun de ses comptes.
//
// Rien ne se déduit du nom. Deux étudiants peuvent s'appeler pareil, et les
// confondre leur donnerait un seul dépôt pour deux : c'est précisément ce que
// le refus des homonymes empêche. Réunir deux comptes est donc une décision,
// prise et écrite, jamais devinée.

// Identity est une personne du groupe : un nom, un matricule, et les comptes
// GitHub sous lesquels elle travaille.
type Identity struct {
	FullName string `json:"full_name"`
	// StudentID est le matricule, quand la liste du collège l'a donné.
	StudentID string `json:"student_id,omitempty"`
	// Accounts porte tous ses comptes ; le premier est celui qui la désigne
	// partout où un seul est attendu.
	Accounts []string `json:"accounts"`
}

// Username rend le compte qui désigne la personne.
func (i Identity) Username() string {
	if len(i.Accounts) == 0 {
		return ""
	}
	return i.Accounts[0]
}

// Designations rend tout ce qui désigne la personne dans une liste de classe,
// sous la forme de Designation : chacun de ses comptes, et son matricule. Un
// compte et un matricule n'y sont jamais confondus, même écrits pareil.
func (i Identity) Designations() []string { return designationsDe(i.Person()) }

// Person rend la personne sous la forme que le reste attend.
func (i Identity) Person() roster.Person {
	autres := append([]string(nil), i.Accounts...)
	if len(autres) > 0 {
		autres = autres[1:]
	}
	return roster.Person{
		FullName: i.FullName, Username: i.Username(),
		StudentID: i.StudentID, Also: autres,
	}
}

// Has dit si un compte est l'un des siens.
func (i Identity) Has(username string) bool {
	for _, compte := range i.Accounts {
		if strings.EqualFold(compte, username) {
			return true
		}
	}
	return false
}

// Identities rend les personnes du groupe, chacune avec tous ses comptes.
//
// Le matricule réunit ce qui doit l'être : deux lignes qui le portent sont la
// même personne, quelles que soient leurs orthographes et leurs comptes. C'est
// la seule chose qui le puisse — le nom ne distingue pas deux homonymes, et
// deux comptes d'une même personne n'ont rien qui les rapproche.
//
// Sans matricule, chaque ligne reste une personne, et seule une déclaration
// explicite les réunit : deux lignes qui portent un même compte — l'une sous
// son nom, l'autre parmi ses autres comptes — sont la même personne. C'est ce
// qu'il advient de deux comptes inscrits chacun de son côté, une fois réunis
// au registre. Rien n'est jamais déduit du nom.
func (c Classroom) Identities() []Identity { return identitiesOf(c.Students) }

// identitiesOf réunit des personnes par leur matricule et par leurs comptes.
func identitiesOf(people []roster.Person) []Identity {
	parMatricule := map[string]int{}
	parCompte := map[string]int{}
	identites := make([]Identity, 0, len(people))
	for _, student := range people {
		matricule := strings.TrimSpace(student.StudentID)
		rang, deja := -1, false
		if matricule != "" {
			rang, deja = parMatricule[strings.ToLower(matricule)]
		}
		for _, compte := range student.Accounts() {
			if deja {
				break
			}
			rang, deja = parCompte[strings.ToLower(compte)]
		}
		if !deja {
			rang = len(identites)
			identites = append(identites, Identity{
				FullName: student.FullName, StudentID: matricule,
				Accounts: student.Accounts(),
			})
		} else {
			// Un nom vide n'efface pas celui qu'on connaît : une ligne sans nom
			// n'apprend rien de plus que ses comptes.
			if strings.TrimSpace(identites[rang].FullName) == "" {
				identites[rang].FullName = student.FullName
			}
			if identites[rang].StudentID == "" {
				identites[rang].StudentID = matricule
			}
			for _, compte := range student.Accounts() {
				if !identites[rang].Has(compte) {
					identites[rang].Accounts = append(identites[rang].Accounts, compte)
				}
			}
		}
		if _, pris := parMatricule[strings.ToLower(matricule)]; matricule != "" && !pris {
			parMatricule[strings.ToLower(matricule)] = rang
		}
		for _, compte := range identites[rang].Accounts {
			parCompte[strings.ToLower(compte)] = rang
		}
	}
	return identites
}

// rang retrouve une personne parmi des identités par ce qui la désigne : l'un
// de ses comptes d'abord, son matricule ensuite (voir Find). Un mot vide ne
// désigne personne.
func rang(identites []Identity, handle string) (int, bool) {
	for position, identite := range identites {
		if identite.Has(handle) {
			return position, true
		}
	}
	matricule := strings.TrimSpace(handle)
	if matricule == "" {
		return 0, false
	}
	for position, identite := range identites {
		if strings.EqualFold(strings.TrimSpace(identite.StudentID), matricule) {
			return position, true
		}
	}
	return 0, false
}

// IdentityOf retrouve la personne à qui un compte appartient, avec tous ses
// autres comptes.
func (c Classroom) IdentityOf(username string) (Identity, bool) {
	for _, identite := range c.Identities() {
		if identite.Has(username) {
			return identite, true
		}
	}
	return Identity{}, false
}

// Accounts rend, pour chaque compte du groupe, tous ceux de la même personne.
// C'est ce qui permet de tenir pour servie une personne dont le dépôt existe,
// sous lequel de ses comptes que ce soit.
func (c Classroom) Accounts() map[string][]string {
	parCompte := map[string][]string{}
	for _, identite := range c.Identities() {
		for _, compte := range identite.Accounts {
			parCompte[strings.ToLower(compte)] = identite.Accounts
		}
	}
	return parCompte
}

// Named rend les comptes que le groupe sait nommer, par ordre alphabétique de
// nom. Il sert aux écrans qui montrent des comptes sans les avoir déjà
// rapprochés — les membres d'une équipe, par exemple.
func (c Classroom) Named() map[string]string {
	noms := map[string]string{}
	for _, student := range c.Students {
		if nom := strings.TrimSpace(student.FullName); nom != "" {
			noms[strings.ToLower(student.Username)] = nom
		}
	}
	for fragment, person := range c.aliases {
		if nom := strings.TrimSpace(person.FullName); nom != "" {
			if _, deja := noms[fragment]; !deja {
				noms[fragment] = nom
			}
		}
	}
	return noms
}

// SortPeople range des personnes comme les écrans les montrent : par nom, puis
// par compte, sans que l'accentuation ne fasse deux rangs d'un seul.
func SortPeople(people []roster.Person) {
	sort.SliceStable(people, func(i, j int) bool {
		gauche := valid.Slugify(people[i].FullName + " " + people[i].Username)
		droite := valid.Slugify(people[j].FullName + " " + people[j].Username)
		return gauche < droite
	})
}
