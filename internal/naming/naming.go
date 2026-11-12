// Package naming compose et relit les noms de dépôts.
//
// Un dépôt porte cinq niveaux, séparés par un point :
//
//	session . cours . groupe . travail . étudiant
//	a26.5n6.01.tp1.emilie-cote
//
// Le point est réservé à cette découpe, et rien d'autre ne peut en produire :
// la slugification remplace tout caractère non alphanumérique par un tiret, si
// bien qu'un nom de cours, de travail ou d'étudiant venu d'un CSV en est
// nettoyé sans qu'on ait à s'en occuper. Un compte GitHub, lui, n'en contient
// jamais.
//
// C'est ce qui distingue cette nomenclature de l'ancienne, tout en tirets : un
// nom se relit sans rien deviner. « a26-5n6-tp1-emilie-cote » ne disait pas où
// finissait le travail et où commençait la personne ; « a26.5n6.01.tp1.emilie-cote »
// le dit.
//
// Le dernier niveau nomme le destinataire du dépôt : l'étudiant pour un travail
// individuel, l'équipe pour un travail d'équipe. Les équipes suivent la même
// découpe, d'un niveau plus court :
//
//	session . cours . groupe . équipe
//	a26.5n6.01.eq1
//
// Seul le nom court d'une session entre dans un dépôt. Son nom long — « Automne
// 2026 » — ne sert qu'à l'affichage et vit dans le fichier des groupes.
package naming

import (
	"strings"

	"github.com/PierreOlivierBrillant/gh-milou/internal/valid"
)

// Separator sépare les niveaux d'un nom de dépôt.
const Separator = "."

// Levels est le nombre de niveaux d'un nom complet.
const Levels = 5

// Parts est un nom de dépôt découpé.
type Parts struct {
	Session    string `json:"session"`
	Course     string `json:"course"`
	Group      string `json:"group"`
	Assignment string `json:"assignment"`
	Student    string `json:"student"`
}

// Prefix est ce qui désigne un groupe : « a26.5n6.01 ».
func Prefix(session, course, group string) string {
	return join(session, course, group)
}

// AssignmentID est ce qui désigne un travail : « a26.5n6.01.tp1 ».
func AssignmentID(session, course, group, assignment string) string {
	return join(session, course, group, assignment)
}

// Compose assemble le nom complet d'un dépôt.
func Compose(session, course, group, assignment, student string) string {
	return join(session, course, group, assignment, student)
}

func join(fragments ...string) string {
	cleaned := make([]string, 0, len(fragments))
	for _, fragment := range fragments {
		cleaned = append(cleaned, strings.TrimSpace(fragment))
	}
	return strings.Join(cleaned, Separator)
}

// Parse découpe un nom de dépôt. Un nom qui n'a pas exactement cinq niveaux
// non vides n'est pas de cette nomenclature : il ne relève pas de l'outil.
func Parse(name string) (Parts, bool) {
	fragments := strings.Split(strings.TrimSpace(name), Separator)
	if len(fragments) != Levels {
		return Parts{}, false
	}
	for _, fragment := range fragments {
		if fragment == "" {
			return Parts{}, false
		}
	}
	return Parts{
		Session: fragments[0], Course: fragments[1], Group: fragments[2],
		Assignment: fragments[3], Student: fragments[4],
	}, true
}

// Belongs dit si un dépôt appartient au groupe donné.
func Belongs(parts Parts, session, course, group string) bool {
	return strings.EqualFold(parts.Session, session) &&
		strings.EqualFold(parts.Course, course) &&
		strings.EqualFold(parts.Group, group)
}

// Fragment valide un niveau saisi à la main — session, cours, groupe, travail.
// La slugification écarte déjà le séparateur ; la vérification qui suit n'est
// là que pour que l'invariant soit dit à l'endroit où il compte.
func Fragment(value, label string) (string, error) {
	slug, err := valid.SlugFragment(value, label)
	if err != nil {
		return "", err
	}
	if strings.Contains(slug, Separator) {
		return "", valid.Errorf(
			"%s : « %s » est réservé à la séparation des niveaux du nom.", label, Separator)
	}
	return slug, nil
}

// Path met en forme un préfixe saisi à la main — « a26.5N6.01 » —, niveau par
// niveau. Le point sépare les niveaux et doit survivre à la mise en forme, qui
// remplacerait sinon tout ce qui n'est pas alphanumérique par un tiret :
// « a26.5n6.01 » deviendrait « a26-5n6-01 », un préfixe que plus rien ne
// retrouve dans les dépôts.
func Path(value, label string) (string, error) {
	niveaux := strings.Split(strings.TrimSpace(value), Separator)
	rendus := make([]string, 0, len(niveaux))
	for _, niveau := range niveaux {
		fragment, err := valid.SlugFragment(niveau, label)
		if err != nil {
			return "", err
		}
		rendus = append(rendus, fragment)
	}
	return strings.Join(rendus, Separator), nil
}

// Assignment met en forme l'identifiant d'un travail saisi à la main.
//
// Celui d'un travail de la nomenclature — « a26.5N6.01.TP1 » — garde ses
// niveaux : c'est sous lui que le registre range ses marques et sa date cible.
// Mis en forme d'un seul tenant, il devenait « a26-5n6-01-tp1 », un préfixe
// hérité que le registre refuse : aucune marque n'y montait. Tout autre
// identifiant est un préfixe hérité — « tp1 », « projet-final » — et se met en
// forme comme avant.
func Assignment(value, label string) (string, error) {
	if chemin, err := Path(value, label); err == nil {
		if _, _, situe := SplitAssignment(chemin); situe {
			return chemin, nil
		}
	}
	return valid.SlugFragment(value, label)
}

// Student compose le fragment d'une personne à partir de son nom complet. Un
// nom complet vide ne donne rien : le dépôt ne peut pas être nommé.
func Student(fullName string) (string, error) {
	slug := valid.Slugify(fullName)
	if slug == "" {
		return "", valid.Errorf(
			"Nom complet manquant : le nom du dépôt en dépend désormais.")
	}
	if len(slug) > valid.MaxSlugLength {
		slug = strings.Trim(slug[:valid.MaxSlugLength], "-")
	}
	return slug, nil
}

// SplitAssignment sépare l'identifiant d'un travail — « a26.5n6.01.tp1 » — en
// la place de son groupe et son nom. Un identifiant qui ne porte pas les quatre
// niveaux de la nomenclature courante ne se coupe pas : le groupe qu'il
// désignerait ne saurait pas nommer un dépôt.
func SplitAssignment(id string) (scope, name string, ok bool) {
	fragments := strings.Split(strings.TrimSpace(id), Separator)
	if len(fragments) != Levels-1 {
		return "", "", false
	}
	for _, fragment := range fragments {
		if fragment == "" {
			return "", "", false
		}
	}
	return Prefix(fragments[0], fragments[1], fragments[2]), fragments[3], true
}

// ------------------------------------------------------------------ équipes

// TeamLevels est le nombre de niveaux du nom d'une équipe : la place du
// groupe, puis le nom court de l'équipe.
const TeamLevels = Levels - 1

// TeamParts est le nom d'une équipe découpé.
type TeamParts struct {
	Session string `json:"session"`
	Course  string `json:"course"`
	Group   string `json:"group"`
	Team    string `json:"team"`
}

// TeamName compose le nom d'une équipe : « a26.5n6.01.eq1 ».
//
// Une équipe GitHub appartient à l'organisation, pas au groupe : deux groupes
// qui voudraient chacun leur « eq1 » se heurteraient, car l'organisation
// n'accepte qu'un « slug » d'équipe donné. La place du groupe est donc écrite
// dans le nom, exactement comme elle l'est dans celui des dépôts — c'est ce qui
// garantit qu'une équipe reste dans son groupe et ne peut pas en changer par
// accident.
func TeamName(session, course, group, team string) string {
	return join(session, course, group, team)
}

// ParseTeam découpe le nom d'une équipe. Un nom qui n'a pas exactement quatre
// niveaux non vides n'est pas de cette nomenclature : l'équipe existe dans
// l'organisation, mais elle ne relève d'aucun groupe.
func ParseTeam(name string) (TeamParts, bool) {
	scope, court, ok := SplitAssignment(name)
	if !ok {
		return TeamParts{}, false
	}
	niveaux := strings.Split(scope, Separator)
	return TeamParts{
		Session: niveaux[0], Course: niveaux[1], Group: niveaux[2], Team: court,
	}, true
}

// TeamBelongs dit si une équipe appartient au groupe donné.
func TeamBelongs(parts TeamParts, session, course, group string) bool {
	return strings.EqualFold(parts.Session, session) &&
		strings.EqualFold(parts.Course, course) &&
		strings.EqualFold(parts.Group, group)
}

// -------------------------------------------------- équipe enseignante

// TeacherTeam est le nom court réservé à l'équipe enseignante d'un groupe :
// « a26.5n6.01.enseignants ».
//
// Elle suit exactement la nomenclature des équipes d'étudiants, et c'est
// voulu : une équipe appartient à l'organisation, et sans la place du groupe
// dans son nom, deux groupes ne pourraient pas avoir chacun la leur. Ce qui la
// distingue tient en un mot réservé — aucune équipe d'étudiants ne peut porter
// ce nom court, et le refus est dit au moment où on la nomme.
//
// C'est elle, et non le registre, qui décide de ce qu'un enseignant voit. Le
// registre dit qui enseigne ; l'équipe dit où. Un collègue absent de l'équipe
// d'un groupe n'accède pas à ses dépôts, quand bien même le registre le
// déclarerait enseignant.
const TeacherTeam = "enseignants"

// TeacherTeamName compose le nom de l'équipe enseignante d'un groupe.
func TeacherTeamName(session, course, group string) string {
	return TeamName(session, course, group, TeacherTeam)
}

// IsTeacherTeam dit si un nom d'équipe découpé est celui d'une équipe
// enseignante.
func IsTeacherTeam(parts TeamParts) bool {
	return strings.EqualFold(parts.Team, TeacherTeam)
}
