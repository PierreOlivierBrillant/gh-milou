package registry

import (
	"strings"

	"github.com/PierreOlivierBrillant/gh-milou/internal/signature"
)

// Holder est la personne à qui une marque a été délivrée, telle que le
// registre sait la nommer aujourd'hui.
type Holder struct {
	// Issued est la ligne du registre des marques : le travail, la date, et ce
	// qui désignait la personne le jour où la marque lui a été délivrée — son
	// compte, ou à défaut son matricule.
	Issued signature.Issued
	// Username, FullName et StudentID disent qui elle est. Un compte rattaché
	// depuis y figure : c'est ce qui garde attribuable une marque délivrée par
	// matricule.
	Username  string
	FullName  string
	StudentID string
	// Team est le nom court de l'équipe qui a reçu la marque, pour un travail
	// d'équipe. Qui la composait ce jour-là, le registre ne le sait pas : les
	// équipes vivent sur GitHub, et leur composition change.
	Team string
}

// Holder rend la personne à qui une marque a été délivrée, et dit si le
// registre la connaît.
//
// La ligne garde ce qui désignait la personne le jour de la délivrance ; ce
// sont les fiches qui disent qui elle est. Une marque délivrée par matricule —
// une cohorte importée du collège, avant qu'on lui rattache des comptes — se
// rapporte ainsi au compte qui porte ce matricule dès que le registre en
// connaît un, sans que la ligne ait à être réécrite.
func (s *Set) Holder(token string) (Holder, bool) {
	ligne, delivree := s.Marks().Who(token)
	if !delivree {
		return Holder{}, false
	}
	titulaire := Holder{Issued: ligne, Username: ligne.Username, StudentID: ligne.StudentID,
		Team: ligne.Team}
	fiche, connue := s.PersonOf(ligne.Username)
	if !connue {
		fiche, connue = s.withStudentID(ligne.StudentID)
		if titulaire.Username == "" {
			titulaire.Username = fiche.Username
		}
	}
	if connue {
		titulaire.FullName = fiche.FullName
		if titulaire.StudentID == "" {
			titulaire.StudentID = fiche.StudentID
		}
	}
	return titulaire, true
}

// withStudentID retrouve la personne qui porte un matricule. Deux comptes qui
// le portent sont la même personne, et c'est celui qui la désigne qui répond.
func (s *Set) withStudentID(matricule string) (User, bool) {
	matricule = strings.TrimSpace(matricule)
	if matricule == "" {
		return User{}, false
	}
	for position, fiche := range s.users {
		if strings.EqualFold(strings.TrimSpace(fiche.StudentID), matricule) {
			return s.whole(position), true
		}
	}
	return User{}, false
}

// Phrase dit à qui, et pour quel travail, la marque a été délivrée : par son
// nom quand le registre le connaît, par son compte à défaut, par son matricule
// en dernier recours — ou par son équipe, pour un travail d'équipe. Le
// navigateur et le terminal la disent mot pour mot.
func (h Holder) Phrase() string {
	pour := " pour « " + h.Issued.Assignment + " »"
	switch {
	case strings.TrimSpace(h.FullName) != "":
		return "délivrée à " + strings.TrimSpace(h.FullName) + pour
	case h.Username != "":
		return "délivrée à @" + h.Username + pour
	case h.Team != "":
		return "délivrée à l'équipe " + h.Team + pour
	default:
		return "délivrée au matricule " + h.StudentID + pour
	}
}
