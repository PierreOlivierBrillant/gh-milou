package users

import (
	"slices"
	"sort"
	"strings"

	"github.com/PierreOlivierBrillant/gh-milou/internal/classroom"
	"github.com/PierreOlivierBrillant/gh-milou/internal/groups"
	"github.com/PierreOlivierBrillant/gh-milou/internal/naming"
	"github.com/PierreOlivierBrillant/gh-milou/internal/teams"
)

// L'annuaire répond à une question que la liste d'un groupe ne peut pas poser :
// qui sont les étudiants de l'organisation, et quels cours ont-ils suivis ?
//
// Un groupe ne connaît que les siens ; l'annuaire les rassemble tous et les
// fond par compte GitHub — c'est lui qui identifie une personne, et c'est
// ainsi qu'on voit qu'Émilie Côté a suivi le même cours à deux sessions.
//
// Il ne montre que les étudiants qu'on connaît : ceux des listes retenues. Un
// dépôt dont le dernier niveau ne correspond à personne n'est pas une personne
// de plus — c'est un nom slugifié, sans compte GitHub derrière. Les compter à
// part est plus honnête que d'inventer quelqu'un ; Unmatched le dit.

// Enrollment est un cours suivi : la place du groupe dans la hiérarchie, et ce
// que les dépôts de la personne y montrent.
type Enrollment struct {
	Scope       string
	Session     string
	SessionName string
	Course      string
	Group       string
	Label       string
	Repos       int
	// PushedAt est le plus récent envoi de la personne dans ce groupe.
	PushedAt string
	// Role dit ce qu'elle y a été. Un même annuaire porte les deux : on ne
	// suit pas le cours qu'on donne, et la ligne doit pouvoir le dire.
	Role string
}

// Teaching dit que la personne a donné ce cours plutôt que de l'avoir suivi.
func (e Enrollment) Teaching() bool { return e.Role == AsTeacher }

// Directory dresse l'annuaire des utilisateurs de plusieurs groupes : une ligne
// par personne, avec les cours qu'elle a suivis ou donnés. Les groupes viennent
// de l'appelant — déclarés ou lus dans les noms de dépôts, l'annuaire ne fait
// pas la différence.
//
// Les enseignants y figurent au même titre que les étudiants, et c'est la seule
// façon de les y faire entrer : un enseignant n'est sur aucune liste de classe.
// Ce sont les équipes enseignantes des groupes qui disent ce qu'il a donné, et
// le registre qui dit qu'il enseigne — « infos » et « known » sont là pour
// cela. Sans eux, l'annuaire ne connaît que les étudiants, ce qu'il faisait
// jusqu'ici.
func Directory(courses []classroom.Classroom, repos []groups.RepoInfo,
	equipes []teams.Team, infos []teams.Info, known Registry) []Row {
	lignes := make([]Row, 0)
	for _, cours := range courses {
		lignes = append(lignes, Build(cours, repos, equipes)...)
	}
	annuaire := together(lignes, known)

	if known != nil {
		// Le rôle vient du registre, jamais des dépôts : rien dans
		// « a26.5n6.01.tp1.emilie-cote » ne dit qui enseigne.
		for position := range annuaire {
			annuaire[position].IsTeacher = teaches(known, annuaire[position])
		}
		annuaire = withTeachers(annuaire, courses, infos, known)
	}

	sort.SliceStable(annuaire, func(i, j int) bool {
		return fallback(annuaire[i], annuaire[j])
	})
	for position := range annuaire {
		sortEnrollments(annuaire[position].Enrollments)
	}
	return annuaire
}

// together réunit les lignes d'une même personne, vues par plusieurs groupes ou
// sous plusieurs comptes.
//
// Deux lignes sont la même personne dès qu'elles ont un compte en commun — ce
// que chaque groupe lui connaît, et ce que le registre a réuni. La clé n'est
// donc pas le compte qui désigne chaque ligne : deux groupes qui déclarent
// chacun l'un de ses deux comptes donnaient deux lignes, et ses cours restaient
// coupés en deux. Rien n'y est déduit du nom.
//
// Le compte qui désigne la personne est celui que le registre désigne, et sa
// ligne passe la première : ce qu'elle dit du nom et du matricule l'emporte.
func together(lignes []Row, known Registry) []Row {
	parent := make([]int, len(lignes))
	for position := range parent {
		parent[position] = position
	}
	racine := func(position int) int {
		for parent[position] != position {
			parent[position] = parent[parent[position]]
			position = parent[position]
		}
		return position
	}
	parCompte := map[string]int{}
	for position, ligne := range lignes {
		for _, compte := range accountsOf(ligne, known) {
			cle := strings.ToLower(compte)
			autre, vu := parCompte[cle]
			if !vu {
				parCompte[cle] = position
				continue
			}
			// La plus ancienne des deux racines reste : l'ordre des groupes
			// décide de celui des lignes, comme avant.
			if a, b := racine(position), racine(autre); a != b {
				parent[max(a, b)] = min(a, b)
			}
		}
	}

	groupes := map[int][]int{}
	ordre := make([]int, 0, len(lignes))
	for position := range lignes {
		cle := racine(position)
		if _, vu := groupes[cle]; !vu {
			ordre = append(ordre, cle)
		}
		groupes[cle] = append(groupes[cle], position)
	}
	annuaire := make([]Row, 0, len(ordre))
	for _, cle := range ordre {
		membres := groupes[cle]
		principal := principalOf(accountsOf(lignes[membres[0]], known), known)
		sort.SliceStable(membres, func(i, j int) bool {
			return holds(lignes[membres[i]].Accounts, principal) &&
				!holds(lignes[membres[j]].Accounts, principal)
		})
		ligne := lignes[membres[0]]
		ligne.Accounts = append([]string(nil), ligne.Accounts...)
		for _, autre := range membres[1:] {
			ligne = fusionner(ligne, lignes[autre])
		}
		annuaire = append(annuaire, designated(ligne, known))
	}
	return annuaire
}

// accountsOf rend tous les comptes d'une ligne : les siens, et ceux que le
// registre leur réunit. Un compte vide n'en est pas un — une liste peut porter
// quelqu'un dont on ne connaît pas encore le compte —, et le prendre pour une
// clé réunirait tous ceux-là en une seule personne.
func accountsOf(ligne Row, known Registry) []string {
	comptes := make([]string, 0, 1+len(ligne.Accounts))
	for _, compte := range append([]string{ligne.Username}, ligne.Accounts...) {
		if strings.TrimSpace(compte) != "" {
			comptes = append(comptes, compte)
		}
	}
	if known == nil {
		return comptes
	}
	for _, compte := range append([]string(nil), comptes...) {
		comptes = append(comptes, known.Accounts(compte)...)
	}
	return comptes
}

// principalOf rend le compte qui désigne la personne au registre, ou rien s'il
// ne la connaît pas.
func principalOf(comptes []string, known Registry) string {
	if known == nil {
		return ""
	}
	for _, compte := range comptes {
		if reunis := known.Accounts(compte); len(reunis) > 0 {
			return reunis[0]
		}
	}
	return ""
}

// designated range les comptes d'une ligne, celui qui désigne la personne
// d'abord, en y ajoutant ceux que le registre lui réunit sans qu'aucun groupe
// ne les connaisse.
func designated(ligne Row, known Registry) Row {
	comptes := accountsOf(ligne, known)
	principal := principalOf(comptes, known)
	if principal == "" {
		return ligne
	}
	ranges := []string{principal}
	for _, compte := range comptes {
		if !holds(ranges, compte) {
			ranges = append(ranges, compte)
		}
	}
	ligne.Username, ligne.Accounts = principal, ranges
	return ligne
}

// teaches dit si l'un des comptes d'une personne est déclaré enseignant. Elle
// en a parfois deux, et le registre peut n'en connaître qu'un.
func teaches(known Registry, ligne Row) bool {
	for _, compte := range append([]string{ligne.Username}, ligne.Accounts...) {
		if compte != "" && known.Teaches(compte) {
			return true
		}
	}
	return false
}

// withTeachers ajoute à l'annuaire les enseignants qui n'y sont pas encore, et
// à ceux qui y sont les cours qu'ils donnent.
//
// Un enseignant ne figure sur aucune liste de classe : sans cet ajout, il
// n'apparaîtrait que s'il a lui-même été étudiant quelque part, et « ne montrer
// que les enseignants » ne montrerait presque personne.
//
// Deux sources, et il faut les deux. Le registre déclare qui enseigne, y
// compris quelqu'un à qui aucun groupe n'a encore été confié. Les équipes
// enseignantes, elles, le prouvent : qui en est a l'accès aux dépôts du
// groupe, et le montrer étudiant serait faux — même si personne ne l'a coopté,
// ce qui ne peut arriver qu'en touchant l'équipe à la main sur GitHub.
//
// Ceux qui sont déjà là — un ancien étudiant devenu collègue — gardent leur
// ligne et ses inscriptions ; seuls les cours donnés s'y ajoutent.
//
// Un enseignant à deux comptes n'a qu'une ligne : ses équipes peuvent porter
// l'un ou l'autre, et le registre dit qu'ils sont à la même personne.
func withTeachers(annuaire []Row, courses []classroom.Classroom, infos []teams.Info,
	known Registry) []Row {
	parCompte := map[string]int{}
	for position, ligne := range annuaire {
		for _, compte := range ligne.Accounts {
			parCompte[strings.ToLower(compte)] = position
		}
	}
	noms := map[string][]string{}
	ordre := make([]string, 0)
	ajouter := func(compte string) {
		compte = strings.TrimSpace(compte)
		if compte == "" {
			return
		}
		comptes := known.Accounts(compte)
		if len(comptes) == 0 {
			comptes = []string{compte}
		}
		cle := strings.ToLower(comptes[0])
		if _, vu := noms[cle]; vu {
			return
		}
		noms[cle] = comptes
		ordre = append(ordre, cle)
	}
	for _, fiche := range known.Teachers() {
		ajouter(fiche.Username)
	}
	for _, cours := range courses {
		if equipe, existe := cours.TeacherTeam(infos); existe {
			for _, membre := range equipe.Members {
				ajouter(membre)
			}
		}
	}

	for _, cle := range ordre {
		comptes := noms[cle]
		donnes := taughtIn(courses, infos, comptes)
		position, connu := -1, false
		for _, compte := range comptes {
			if position, connu = parCompte[strings.ToLower(compte)]; connu {
				break
			}
		}
		if !connu {
			for _, compte := range comptes {
				parCompte[strings.ToLower(compte)] = len(annuaire)
			}
			annuaire = append(annuaire, Row{
				FullName: known.Name(comptes[0]), Username: comptes[0],
				Accounts: comptes, IsTeacher: true,
				Repos: []Repo{}, Enrollments: donnes,
			})
			continue
		}
		annuaire[position].IsTeacher = true
		if annuaire[position].FullName == "" {
			annuaire[position].FullName = known.Name(comptes[0])
		}
		annuaire[position].Enrollments = append(annuaire[position].Enrollments, donnes...)
	}
	return annuaire
}

// taughtIn énumère les groupes qu'une personne enseigne, sous l'un ou l'autre
// de ses comptes, lus dans leurs équipes enseignantes. C'est la seule trace
// qu'un enseignement laisse : rien ne l'inscrit ailleurs.
func taughtIn(courses []classroom.Classroom, infos []teams.Info,
	accounts []string) []Enrollment {
	donnes := make([]Enrollment, 0)
	for _, cours := range courses {
		equipe, existe := cours.TeacherTeam(infos)
		if !existe || !slices.ContainsFunc(accounts, equipe.Has) {
			continue
		}
		inscription := Enrollment{
			Scope: cours.Scope(), Session: cours.Session, Course: cours.Course,
			Group: cours.Group, Label: cours.Label(), Role: AsTeacher,
		}
		if cours.Session != "" {
			inscription.SessionName = cours.SessionName()
		}
		donnes = append(donnes, inscription)
	}
	return donnes
}

// sortEnrollments range les cours suivis de la session la plus récente à la
// plus ancienne : c'est l'ordre où l'on cherche quelqu'un, et il est décidé
// ici pour que le terminal et le navigateur montrent le même.
func sortEnrollments(inscriptions []Enrollment) {
	sort.SliceStable(inscriptions, func(i, j int) bool {
		if rang := naming.CompareSessions(
			inscriptions[i].Session, inscriptions[j].Session); rang != 0 {
			return rang < 0
		}
		if inscriptions[i].Course != inscriptions[j].Course {
			return strings.ToLower(inscriptions[i].Course) <
				strings.ToLower(inscriptions[j].Course)
		}
		return strings.ToLower(inscriptions[i].Group) < strings.ToLower(inscriptions[j].Group)
	})
}

// Unmatched compte les dépôts que l'annuaire n'a pu rattacher à personne : ils
// suivent la nomenclature, mais leur dernier niveau ne désigne aucun étudiant
// connu. C'est ce qu'il manque pour que l'annuaire soit complet — le dire
// évite de lire une liste trouée comme si elle était entière.
func Unmatched(courses []classroom.Classroom, repos []groups.RepoInfo,
	equipes []teams.Team) int {
	orphelins := 0
	for _, cours := range courses {
		for _, travail := range cours.Assignments(repos, equipes) {
			orphelins += travail.Others
		}
	}
	return orphelins
}

// Orphans nomme, sur tous les groupes, les derniers niveaux que personne ne
// réclame — ce que « Unmatched » ne fait que compter. C'est en les nommant
// qu'on peut les rattacher, et l'annuaire est le bon grain pour le faire : il
// est à l'échelle de l'organisation, comme le registre.
func Orphans(courses []classroom.Classroom, repos []groups.RepoInfo,
	equipes []teams.Team) []classroom.Orphan {
	listes := make([][]classroom.Orphan, 0, len(courses))
	for _, cours := range courses {
		listes = append(listes, cours.Orphans(repos, equipes))
	}
	return classroom.MergeOrphans(listes...)
}

// fusionner réunit deux lignes de la même personne, vues par deux groupes.
func fusionner(gauche, droite Row) Row {
	if gauche.FullName == "" {
		gauche.FullName = droite.FullName
	}
	if gauche.StudentID == "" {
		gauche.StudentID = droite.StudentID
	}
	// Les comptes d'une personne sont ceux que tous ses groupes lui
	// connaissent. Un groupe où elle n'a déclaré qu'un compte ne doit pas
	// faire oublier le second qu'un autre a retenu.
	for _, compte := range droite.Accounts {
		if !holds(gauche.Accounts, compte) {
			gauche.Accounts = append(gauche.Accounts, compte)
		}
	}
	gauche.Repos = append(gauche.Repos, droite.Repos...)
	gauche.Enrollments = append(gauche.Enrollments, droite.Enrollments...)
	if droite.PushedAt > gauche.PushedAt {
		gauche.PushedAt = droite.PushedAt
	}
	return gauche
}

// holds dit si un compte figure déjà dans une liste, casse ignorée.
func holds(comptes []string, valeur string) bool {
	for _, compte := range comptes {
		if strings.EqualFold(compte, valeur) {
			return true
		}
	}
	return false
}

// enrollmentOf décrit l'inscription d'une personne à un groupe. Elle vaut même
// sans dépôt : être du groupe est ce que la liste dit, pas ce que GitHub
// montre.
func enrollmentOf(cours classroom.Classroom, depots []Repo) Enrollment {
	inscription := Enrollment{
		Scope: cours.Scope(), Session: cours.Session, Course: cours.Course,
		Group: cours.Group, Label: cours.Label(), Repos: len(depots),
		Role: AsStudent,
	}
	if cours.Session != "" {
		inscription.SessionName = cours.SessionName()
	}
	for _, depot := range depots {
		if depot.PushedAt > inscription.PushedAt {
			inscription.PushedAt = depot.PushedAt
		}
	}
	return inscription
}

// SessionsIn énumère les sessions de l'annuaire, de la plus récente à la plus
// ancienne — le même ordre que partout ailleurs.
func SessionsIn(rows []Row) []classroom.Session {
	courts := make([]string, 0, len(rows))
	for _, ligne := range rows {
		for _, inscription := range ligne.Enrollments {
			courts = append(courts, inscription.Session)
		}
	}
	return classroom.SessionsOf(courts)
}

// CoursesIn énumère les cours de l'annuaire, par ordre alphabétique. Une
// session donnée les restreint à ceux qu'elle porte : choisir une session puis
// un cours ne doit pas proposer un cours qui n'y a pas eu lieu.
func CoursesIn(rows []Row, session string) []string {
	vus := map[string]string{}
	for _, ligne := range rows {
		for _, inscription := range ligne.Enrollments {
			if inscription.Course == "" {
				continue
			}
			if session != "" && !strings.EqualFold(inscription.Session, session) {
				continue
			}
			vus[strings.ToLower(inscription.Course)] = inscription.Course
		}
	}
	trouves := make([]string, 0, len(vus))
	for _, cours := range vus {
		trouves = append(trouves, cours)
	}
	sort.Slice(trouves, func(i, j int) bool {
		return strings.ToLower(trouves[i]) < strings.ToLower(trouves[j])
	})
	return trouves
}
