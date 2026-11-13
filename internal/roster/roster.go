// Package roster charge et valide la liste des personnes (nom complet + compte GitHub).
package roster

import (
	"encoding/csv"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/PierreOlivierBrillant/gh-milou/internal/valid"
)

// En-têtes reconnus, en français comme en anglais.
//
// « Nom » désigne le nom complet quand la liste n'a pas de colonne de prénom,
// et le nom de famille quand elle en a une — c'est le cas d'une liste sortie
// d'Omnivox, qui les sépare. La présence d'un prénom tranche donc la lecture
// des noms, et rien d'autre n'a besoin d'être deviné.
var nameHeaders = map[string]bool{
	"nom": true, "nom complet": true, "nom_complet": true, "name": true,
	"full_name": true, "fullname": true, "etudiant": true, "étudiant": true,
	"nom de l'étudiant": true, "nom de l'etudiant": true, "last_name": true,
}

var firstHeaders = map[string]bool{
	"prénom": true, "prenom": true, "prénom de l'étudiant": true,
	"prenom de l'etudiant": true, "first_name": true, "first": true,
}

var loginHeaders = map[string]bool{
	"github": true, "github_username": true, "username": true, "login": true,
	"compte": true, "utilisateur": true, "handle": true,
	"compte github": true, "nom d'utilisateur github": true,
}

var studentHeaders = map[string]bool{
	"no étudiant": true, "no etudiant": true, "no. étudiant": true,
	"numéro d'étudiant": true, "numero d'etudiant": true, "matricule": true,
	"student_id": true, "no de l'étudiant": true,
}

var permanentHeaders = map[string]bool{
	"code perm.": true, "code perm": true, "code permanent": true,
	"permanent": true, "code_permanent": true,
}

var groupHeaders = map[string]bool{
	"groupe": true, "no groupe": true, "numéro de groupe": true, "group": true,
}

// Séparateurs de colonnes reconnus.
var delimiters = []rune{',', ';', '\t'}

// Person est une personne de la cohorte.
type Person struct {
	FullName string `json:"full_name"`
	Username string `json:"username"`
	// StudentID est le matricule, tel que la liste du collège le donne. C'est
	// la seule chose qui identifie vraiment quelqu'un : deux étudiants peuvent
	// s'appeler pareil, et un même étudiant peut travailler sous deux comptes.
	// Le nom ne tranche ni l'un ni l'autre ; le matricule tranche les deux.
	StudentID string `json:"student_id,omitempty"`
	// Also porte les autres comptes GitHub de la même personne : celui du
	// collège et le sien, celui que GitHub Classroom lui a fait créer et celui
	// qu'elle avait déjà.
	//
	// Rien ne se déduit du nom : deux étudiants peuvent s'appeler pareil, et
	// les confondre leur donnerait un seul dépôt pour deux. C'est donc une
	// décision, prise et écrite, jamais devinée.
	Also []string `json:"also,omitempty"`
}

// Accounts rend tous les comptes de la personne, le sien d'abord.
func (p Person) Accounts() []string {
	comptes := make([]string, 0, 1+len(p.Also))
	vus := map[string]bool{}
	for _, compte := range append([]string{p.Username}, p.Also...) {
		compte = strings.TrimSpace(compte)
		if compte == "" || vus[strings.ToLower(compte)] {
			continue
		}
		vus[strings.ToLower(compte)] = true
		comptes = append(comptes, compte)
	}
	return comptes
}

// Owns dit si un compte est l'un des siens.
func (p Person) Owns(username string) bool {
	for _, compte := range p.Accounts() {
		if strings.EqualFold(compte, username) {
			return true
		}
	}
	return false
}

// Key sert au dédoublonnage : le compte GitHub est insensible à la casse.
func (p Person) Key() string { return strings.ToLower(p.Username) }

// String affiche la personne de façon lisible.
func (p Person) String() string { return p.FullName + " <@" + p.Username + ">" }

// Validate met une personne en forme et refuse ce qui ne peut pas la désigner.
// Le nom complet, lui, peut manquer : il se retrouve ensuite depuis le profil
// GitHub, et son absence n'empêche que de nommer un dépôt.
func (p Person) Validate() (Person, error) {
	username, err := valid.Login(p.Username, "Compte GitHub")
	if err != nil {
		return p, err
	}
	fullName := strings.TrimSpace(p.FullName)
	if fullName != "" {
		if fullName, err = valid.FullName(fullName); err != nil {
			return p, err
		}
	}
	autres := make([]string, 0, len(p.Also))
	vus := map[string]bool{strings.ToLower(username): true}
	for _, brut := range p.Also {
		if strings.TrimSpace(brut) == "" {
			continue
		}
		compte, err := valid.Login(brut, "Autre compte GitHub")
		if err != nil {
			return p, err
		}
		if vus[strings.ToLower(compte)] {
			continue
		}
		vus[strings.ToLower(compte)] = true
		autres = append(autres, compte)
	}
	if len(autres) == 0 {
		autres = nil
	}
	return Person{
		FullName: fullName, Username: username,
		StudentID: strings.TrimSpace(p.StudentID), Also: autres,
	}, nil
}

// Issue décrit un problème détecté sur une ligne de la liste.
type Issue struct {
	Line    int    `json:"line"`
	Raw     string `json:"raw"`
	Message string `json:"message"`
}

// Entry est une ligne d'une liste : une personne, et ce que la liste dit d'elle
// en plus.
//
// Le compte GitHub peut manquer. Une liste sortie d'Omnivox ne le connaît pas —
// elle porte un numéro d'étudiant, un nom, un code permanent —, et c'est le
// rapprochement qui le trouve ensuite dans les dépôts. Ce qui l'entoure sert
// justement à ce rapprochement : rien n'y est retenu pour être affiché.
type Entry struct {
	FullName  string `json:"full_name"`
	Username  string `json:"username"`
	StudentID string `json:"student_id,omitempty"`
	Permanent string `json:"permanent,omitempty"`
	Group     string `json:"group,omitempty"`
}

// Person rend la personne que l'entrée décrit.
func (e Entry) Person() Person {
	return Person{
		FullName: e.FullName, Username: e.Username, StudentID: e.StudentID,
	}
}

// Roster est le résultat d'un chargement : les lignes lues et celles rejetées.
type Roster struct {
	// Entries porte tout ce que la liste disait, compte GitHub compris quand
	// elle en avait un.
	Entries []Entry
	// People ne retient que les entrées qui ont un compte : c'est avec elles
	// qu'on peut créer des dépôts sans rien avoir à rapprocher.
	People []Person
	Issues []Issue
}

// Named dit si la liste porte des comptes GitHub. Une liste qui n'en a aucun
// n'est pas fautive : elle demande un rapprochement avant de servir.
func (r Roster) Named() bool { return len(r.People) > 0 }

// WithAccounts ne retient d'une liste que les personnes dont on connaît le
// compte GitHub.
//
// C'est ce qu'il faut à qui interroge GitHub à leur sujet. Vérifier l'existence
// d'un compte qu'on n'a pas revient à demander « @ » à l'API, qui répond non :
// une cohorte fraîchement importée du collège paraissait alors n'avoir que des
// comptes inexistants, et la création s'arrêtait là.
func WithAccounts(people []Person) []Person {
	nommees := make([]Person, 0, len(people))
	for _, person := range people {
		if strings.TrimSpace(person.Username) != "" {
			nommees = append(nommees, person)
		}
	}
	return nommees
}

// Everyone rend toutes les personnes de la liste, celles dont le compte GitHub
// manque comprises.
//
// C'est ce qu'il faut à qui inscrit une cohorte, et « People » ne peut pas y
// servir : un export d'Omnivox ne connaît aucun compte, si bien que « People »
// n'en retiendrait personne — une liste de vingt-trois noms se lisait alors
// comme une liste vide, sans un mot pour le dire, puisque rien dans ce fichier
// n'est fautif. Le matricule désigne déjà chacun ; le compte se rattache
// quand on le connaît.
func (r Roster) Everyone() []Person {
	people := make([]Person, 0, len(r.Entries))
	for _, entree := range r.Entries {
		people = append(people, entree.Person())
	}
	return people
}

// IsValid indique une liste exploitable sans aucun rejet.
func (r Roster) IsValid() bool { return len(r.Issues) == 0 && len(r.Entries) > 0 }

// sniffDelimiter devine le séparateur de colonnes, avec la virgule comme repli.
func sniffDelimiter(text string) rune {
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		best, bestCount := ',', 0
		for _, candidate := range delimiters {
			if count := strings.Count(line, string(candidate)); count > bestCount {
				best, bestCount = candidate, count
			}
		}
		if bestCount > 0 {
			return best
		}
		return ','
	}
	return ','
}

func normalizeHeader(value string) string {
	cleaned := strings.TrimPrefix(value, "\ufeff")
	return strings.ToLower(strings.TrimSpace(cleaned))
}

// columns dit où lire chaque renseignement dans une ligne. Une colonne absente
// vaut -1 ; seul le nom est indispensable.
type columns struct {
	name, first, login, student, permanent, group int
	// found dit que des en-têtes ont été reconnus. Sans eux, la lecture retombe
	// sur deux colonnes dans l'ordre — nom, puis compte —, ce qu'une liste
	// écrite à la main peut légitimement être.
	found bool
}

// detectColumns repère dans une ligne d'en-tête ce qu'elle sait donner.
func detectColumns(row []string) columns {
	trouvees := columns{name: -1, first: -1, login: -1, student: -1, permanent: -1, group: -1}
	place := func(cible *int, index int) {
		if *cible < 0 {
			*cible = index
		}
	}
	for index, cell := range row {
		header := normalizeHeader(unarmor(cell))
		switch {
		case nameHeaders[header]:
			place(&trouvees.name, index)
		case firstHeaders[header]:
			place(&trouvees.first, index)
		case loginHeaders[header]:
			place(&trouvees.login, index)
		case studentHeaders[header]:
			place(&trouvees.student, index)
		case permanentHeaders[header]:
			place(&trouvees.permanent, index)
		case groupHeaders[header]:
			place(&trouvees.group, index)
		}
	}
	// Un nom seul ne suffit pas à reconnaître un en-tête : la première ligne
	// d'une liste sans en-tête porterait un nom, elle aussi. Il faut qu'une
	// autre colonne se soit fait reconnaître avec lui.
	autre := trouvees.first >= 0 || trouvees.login >= 0 || trouvees.student >= 0 ||
		trouvees.permanent >= 0 || trouvees.group >= 0
	trouvees.found = trouvees.name >= 0 && autre
	if !trouvees.found {
		return columns{name: 0, first: -1, login: 1, student: -1, permanent: -1, group: -1}
	}
	return trouvees
}

// at rend la cellule d'une colonne, vide si la colonne manque ou si la ligne
// est trop courte.
func at(cells []string, index int) string {
	if index < 0 || index >= len(cells) {
		return ""
	}
	return cells[index]
}

// ParseBytes lit un contenu brut, quel que soit son encodage : c'est par ici
// qu'entre un fichier déposé dans le navigateur, dont on n'a que les octets.
func ParseBytes(content []byte) Roster { return Parse(decode(content)) }

// Parse analyse un contenu CSV/TSV et renvoie les personnes valides et les erreurs.
func Parse(text string) Roster {
	text = strings.TrimPrefix(text, "\ufeff")
	if strings.TrimSpace(text) == "" {
		return Roster{Issues: []Issue{{Message: "Le fichier est vide."}}}
	}

	delimiter := sniffDelimiter(text)
	reader := csv.NewReader(strings.NewReader(text))
	reader.Comma = delimiter
	reader.FieldsPerRecord = -1 // les lignes courtes sont signalées, pas rejetées par le lecteur
	reader.LazyQuotes = true
	reader.TrimLeadingSpace = true

	var rows [][]string
	for {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			// Une ligne illisible ne doit pas interrompre le reste du fichier.
			rows = append(rows, nil)
			continue
		}
		rows = append(rows, row)
	}

	result := Roster{}
	seen := map[string]Person{}

	colonnes := columns{name: 0, first: -1, login: 1, student: -1, permanent: -1, group: -1}
	start := 0
	if len(rows) > 0 {
		if trouvees := detectColumns(rows[0]); trouvees.found {
			colonnes, start = trouvees, 1
		}
	}
	// Sans colonne de compte, la liste n'en promet aucun : une ligne qui n'en
	// porte pas n'est pas fautive. Avec une telle colonne, elle l'est.
	attendu := colonnes.login >= 0

	for offset := start; offset < len(rows); offset++ {
		row := rows[offset]
		line := offset + 1
		raw := strings.Join(row, string(delimiter))

		cells := make([]string, len(row))
		empty := true
		for index, cell := range row {
			cells[index] = unarmor(cell)
			if cells[index] != "" {
				empty = false
			}
		}
		if empty || strings.HasPrefix(cells[0], "#") {
			continue // ligne vide ou commentaire
		}

		nom := at(cells, colonnes.name)
		// Nom et prénom séparés : c'est la forme d'Omnivox, et le prénom passe
		// devant — c'est ainsi qu'on nomme une personne, et ainsi que son nom
		// entrera dans celui de ses dépôts.
		if prenom := at(cells, colonnes.first); prenom != "" {
			nom = strings.TrimSpace(prenom + " " + nom)
		}
		fullName, err := valid.FullName(nom)
		if err != nil {
			result.Issues = append(result.Issues, Issue{line, raw, err.Error()})
			continue
		}

		entree := Entry{
			FullName:  fullName,
			StudentID: at(cells, colonnes.student),
			Permanent: at(cells, colonnes.permanent),
			Group:     at(cells, colonnes.group),
		}

		brut := at(cells, colonnes.login)
		if brut == "" && !attendu {
			result.Entries = append(result.Entries, entree)
			continue
		}
		username, err := valid.Login(brut, "")
		if err != nil {
			result.Issues = append(result.Issues, Issue{line, raw, err.Error()})
			continue
		}
		entree.Username = username

		if previous, exists := seen[strings.ToLower(username)]; exists {
			result.Issues = append(result.Issues, Issue{line, raw,
				"Compte « " + username + " » déjà présent pour « " + previous.FullName + " »."})
			continue
		}
		seen[strings.ToLower(username)] = entree.Person()
		result.Entries = append(result.Entries, entree)
		result.People = append(result.People, entree.Person())
	}

	if len(result.Entries) == 0 && len(result.Issues) == 0 {
		result.Issues = append(result.Issues, Issue{Message: "Aucune personne trouvée dans le fichier."})
	}
	return result
}

// Load charge une liste depuis un fichier CSV/TSV.
func Load(path string) (Roster, error) {
	expanded, err := ExpandPath(path)
	if err != nil {
		return Roster{}, err
	}
	info, err := os.Stat(expanded)
	if err != nil || info.IsDir() {
		return Roster{}, valid.Errorf("Fichier introuvable : %s", expanded)
	}
	content, err := os.ReadFile(expanded)
	if err != nil {
		return Roster{}, valid.Errorf("Fichier illisible : %v", err)
	}
	// L'encodage n'est plus une condition : une liste sortie d'Omnivox arrive
	// en Windows-1252, et la refuser obligerait à la convertir à la main.
	return ParseBytes(content), nil
}

// Write écrit une liste de personnes au format CSV.
func Write(path string, people []Person) (string, error) {
	expanded, err := ExpandPath(path)
	if err != nil {
		return "", err
	}
	if parent := filepath.Dir(expanded); parent != "" {
		if err := os.MkdirAll(parent, 0o755); err != nil {
			return "", valid.Errorf("Enregistrement impossible : %v", err)
		}
	}
	file, err := os.Create(expanded)
	if err != nil {
		return "", valid.Errorf("Enregistrement impossible : %v", err)
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	records := [][]string{{"nom_complet", "github_username"}}
	for _, person := range people {
		records = append(records, []string{person.FullName, person.Username})
	}
	if err := writer.WriteAll(records); err != nil {
		return "", valid.Errorf("Enregistrement impossible : %v", err)
	}
	return expanded, nil
}

// ExpandPath développe « ~ » et rend le chemin utilisable tel quel.
func ExpandPath(path string) (string, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return "", valid.Errorf("Chemin : la valeur est vide.")
	}
	if trimmed == "~" || strings.HasPrefix(trimmed, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", valid.Errorf("Chemin : dossier personnel introuvable (%v).", err)
		}
		trimmed = filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(trimmed, "~"), "/"))
	}
	return trimmed, nil
}

// GitHub refuse deux dépôts de même nom dans une organisation. Quand celui
// qu'on demande est déjà pris — le même travail distribué deux fois à la même
// personne, ou deux groupes qui nomment leur travail pareil —, il ajoute
// « -1 », puis « -2 ». La marque se pose à la fin du nom du dépôt, donc sur le
// compte quand c'est lui qui le termine, et adopter une organisation la lisait
// comme si elle en faisait partie : « aleksilesaj-1 » n'est le compte de
// personne.
var duplicateMarker = regexp.MustCompile(`-[0-9]+$`)

// WithoutDuplicateMarker retire d'un compte la marque de doublon de GitHub. Le
// booléen dit qu'il y en avait une, rien de plus : « CO-7 » est un vrai compte
// et « aleksilesaj-1 » n'en est pas un, or les deux se terminent pareil. Seul
// ce qui atteste le compte sans la marque les distingue, et c'est à l'appelant
// de le dire.
func WithoutDuplicateMarker(username string) (string, bool) {
	login := strings.TrimSpace(username)
	base := duplicateMarker.ReplaceAllString(login, "")
	if base == "" || base == login {
		return login, false
	}
	return base, true
}

// SameName dit que deux noms complets peuvent être ceux d'une même personne :
// le même, ou l'un des deux encore inconnu. C'est ce qui autorise à fondre deux
// fiches que la marque de doublon avait séparées.
func SameName(left, right string) bool {
	left, right = strings.TrimSpace(left), strings.TrimSpace(right)
	return left == "" || right == "" || strings.EqualFold(left, right)
}
