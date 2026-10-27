package registry

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/PierreOlivierBrillant/gh-milou/internal/brand"
	"github.com/PierreOlivierBrillant/gh-milou/internal/cache"
	"github.com/PierreOlivierBrillant/gh-milou/internal/exchange"
	"github.com/PierreOlivierBrillant/gh-milou/internal/ghapi"
	"github.com/PierreOlivierBrillant/gh-milou/internal/rules"
	"github.com/PierreOlivierBrillant/gh-milou/internal/signature"
	"github.com/PierreOlivierBrillant/gh-milou/internal/valid"
)

// Le registre s'écrit par échange conditionnel, jamais par fusion.
//
// Celui qui écrit lit la tête de la branche, fabrique un commit qui en descend,
// puis demande à GitHub de faire avancer la référence sans forcer. GitHub
// refuse alors tout ce qui n'est pas une avance rapide : si quelqu'un a écrit
// entre-temps, le refus tombe, et l'écriture est refaite sur l'état frais.
//
// Ce qui est rejoué est le changement — « ces personnes sont connues » —, non
// le registre qu'on avait en main. Le travail de l'autre est donc conservé
// entier. Aucun clone, aucune branche, aucun « git merge » : il n'y a rien à
// résoudre, donc rien qui puisse rester en conflit.

// Attempts borne les reprises. Cinq suffisent très largement : il faudrait que
// cinq écritures se glissent coup sur coup entre notre lecture et la nôtre.
// Au-delà, ce n'est plus de la concurrence, c'est une panne.
const Attempts = 5

// Description est ce que le dépôt du registre annonce sur github.com.
const Description = "Registre des utilisateurs — " + brand.Command + ". Privé : contient des renseignements personnels."

// step nomme l'étape qui a échoué, sans perdre l'erreur d'origine : son statut
// HTTP et la portée qui lui manque servent encore en aval.
//
// « HTTP 409 — Git Repository is empty. » ne disait pas ce qu'on faisait au
// moment où il est tombé, et cela a coûté cher à diagnostiquer. Une erreur du
// registre doit dire de quel dépôt et de quelle étape elle vient.
func (s *Store) step(quoi string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s de « %s/%s » : %w", quoi, s.org, RepoName, err)
}

// Store lit et écrit le registre d'une organisation.
type Store struct {
	client *ghapi.Client
	org    string
	now    func() time.Time
	// local retient ce qu'on a déjà lu, scellé par le commit d'où il vient.
	// Il peut être nil : le registre se lit alors toujours depuis GitHub.
	local *cache.Cache

	// mutex sérialise les écritures de ce processus. L'interface web sert
	// plusieurs onglets : deux d'entre eux ne doivent pas se disputer la tête
	// de la branche avant même d'atteindre GitHub, où ils s'excluraient au prix
	// d'un aller-retour.
	mutex sync.Mutex

	// recent retient la dernière lecture et son heure (voir Recheck). Il a son
	// propre verrou : une lecture n'a pas à attendre qu'une écriture, qui
	// tient « mutex » le temps de ses allers-retours, soit finie.
	recentMutex sync.Mutex
	recent      Snapshot
	recentAt    time.Time
}

// Recheck est le temps pendant lequel une lecture du registre vaut sans qu'on
// relève la tête de sa branche.
//
// Un seul écran demande les noms plusieurs fois — pour lister les groupes, pour
// dater les envois, pour ouvrir un groupe —, et chaque demande coûtait un
// aller-retour à GitHub pour s'entendre dire que rien n'avait bougé : une
// demi-seconde au moins par écran, cache plein ou pas. Ce qui s'écrit depuis
// ce processus est retenu aussitôt ; ce qu'un collègue écrit ailleurs se voit
// au plus une minute plus tard. Une écriture, elle, relève toujours la tête
// réelle : elle ne passe pas par ici.
const Recheck = time.Minute

// New ouvre le registre d'une organisation. Le cache peut être nil.
func New(client *ghapi.Client, org string, local *cache.Cache) *Store {
	return &Store{client: client, org: org, now: time.Now, local: local}
}

// Snapshot est le registre tel qu'il était à un commit donné.
type Snapshot struct {
	Set *Set
	// Head est le commit d'où il vient ; vide quand rien n'a encore été écrit.
	// C'est lui qui dit si le registre a bougé depuis la dernière lecture.
	Head string
	// Issues énumère ce que la lecture a dû écarter. Le fichier se modifie à la
	// main sur github.com : une fiche mal écrite doit se signaler, pas priver
	// toute l'organisation de ses noms.
	Issues []string
	// Seeded dit que le fichier du registre est déjà dans le dépôt. Sans lui,
	// le prochain commit doit aussi y déposer de quoi l'expliquer.
	Seeded bool
	// Stale dit que GitHub n'a pas répondu et que ce registre vient du disque.
	// L'appelant doit le montrer : afficher des noms périmés en silence serait
	// pire que d'en afficher aucun.
	Stale bool
}

// keptSchema est la forme de ce que le cache retient. Elle change dès qu'un
// champ de « keptSet » change de nom ou de sens.
//
// Sans elle, une entrée écrite par une version antérieure se relisait en
// silence, ses champs inconnus à zéro : un registre vide scellé par le bon
// commit, donc tenu pour à jour, et pour trois mois. Les noms que le fichier
// local ne redit plus — il les retire dès que le registre les porte —
// disparaissaient alors de l'écran ; et un compte sans nom ne peut plus être
// rattaché à ses dépôts, dont le dernier niveau est ce nom slugifié.
//
// C'est arrivé au renommage de « students » en « users ». Une entrée d'une
// forme qu'on ne reconnaît pas n'est donc plus lue du tout : relire GitHub
// coûte une requête, se tromper coûtait bien davantage.
// La version 6 ajoute les demandes de levée du voile, la 7 ce que la lecture a
// dû signaler, la 8 le renvoi d'un compte à un autre de la même personne.
const keptSchema = 8

// keptSet est ce que le cache local retient : le registre, et le commit qui le
// scelle. Tant que la branche pointe sur ce commit, ce contenu vaut toujours.
//
// Les deux sections sont retenues ensemble parce qu'un seul commit les scelle :
// en garder une seule obligerait à relire l'autre pour rien.
type keptSet struct {
	// Schema dit de quelle forme vient cette entrée. Une entrée sans lui vient
	// d'avant qu'on les distingue, et ne se relit pas.
	Schema      int              `json:"schema"`
	Head        string           `json:"head"`
	Users       []User           `json:"users"`
	Assignments []Assignment     `json:"assignments,omitempty"`
	Rules       rules.Rules      `json:"rules,omitzero"`
	Catalog     exchange.Catalog `json:"catalog,omitzero"`
	Marks       signature.Book   `json:"marks,omitzero"`
	Asks        exchange.Asks    `json:"asks,omitzero"`
	// Issues redit ce que la lecture avait dû signaler. Sans lui, un registre
	// écrit par une version ultérieure — dont cette version-ci ne sait relire
	// qu'une partie, parfois rien — s'affichait sans un mot dès la deuxième
	// fois : l'avertissement ne tenait que le temps de la lecture qui l'avait
	// produit, et le sceau vaut trois mois. C'est un avertissement dont le
	// silence coûte cher, puisqu'il dit justement de mettre l'outil à jour.
	Issues []string `json:"issues,omitempty"`
}

// Load lit le registre.
//
// Un dépôt absent, ou un registre pas encore amorcé, donne un registre vide
// sans erreur : une organisation où l'on n'a rien écrit n'est pas une panne.
// Le fichier est lu au commit exact qu'on vient de relever, non à la branche :
// entre les deux requêtes, quelqu'un peut avoir écrit, et la lecture doit
// rester d'une seule pièce.
func (s *Store) Load() (Snapshot, error) {
	if recent, valable := s.recently(); valable {
		return recent, nil
	}
	return s.load(true)
}

// recently rend la dernière lecture si elle a moins de « Recheck ». Sans cache
// local, il n'y en a jamais : un cache désactivé demande que tout soit relu.
func (s *Store) recently() (Snapshot, bool) {
	if s.local == nil {
		return Snapshot{}, false
	}
	s.recentMutex.Lock()
	defer s.recentMutex.Unlock()
	if s.recentAt.IsZero() || s.now().Sub(s.recentAt) >= Recheck {
		return Snapshot{}, false
	}
	return s.recent, true
}

// remember retient une lecture que GitHub vient de confirmer. Un registre
// relu du disque faute de réseau ne l'est pas : la prochaine lecture doit
// retenter GitHub, pas répéter la panne pendant une minute.
func (s *Store) remember(snapshot Snapshot) {
	if s.local == nil || snapshot.Stale {
		return
	}
	s.recentMutex.Lock()
	defer s.recentMutex.Unlock()
	s.recent, s.recentAt = snapshot, s.now()
}

// unremember oublie la dernière lecture : la tête vient de changer sans que le
// contenu qu'on retient le dise.
func (s *Store) unremember() {
	s.recentMutex.Lock()
	defer s.recentMutex.Unlock()
	s.recent, s.recentAt = Snapshot{}, time.Time{}
}

// load lit le registre ; « offline » autorise le repli sur le disque.
//
// Une écriture ne s'en autorise jamais : elle a besoin de la tête réelle de la
// branche, et écrire depuis une lecture périmée serait de toute façon refusé.
// Ce qu'elle relève sert quand même aux lectures qui suivent.
func (s *Store) load(offline bool) (Snapshot, error) {
	snapshot, err := s.read(offline)
	if err == nil {
		s.remember(snapshot)
	}
	return snapshot, err
}

// read relève la tête de la branche, et ne retélécharge le registre que si
// elle a bougé depuis ce que le disque retient.
func (s *Store) read(offline bool) (Snapshot, error) {
	head, err := s.client.BranchHead(s.org, RepoName, Branch)
	if err != nil {
		// Seule une panne de liaison justifie le repli. Un refus de GitHub —
		// jeton expiré, droit manquant — doit remonter tel quel : montrer des
		// noms périmés au lieu de dire « votre jeton a expiré » égarerait.
		if offline && ghapi.StatusOf(err) == 0 {
			if garde, connu := s.kept(); connu {
				garde.Stale = true
				return garde, nil
			}
		}
		return Snapshot{}, s.step("lecture de la branche", err)
	}
	if head == "" {
		return Snapshot{Set: Empty()}, nil
	}
	// Le commit relevé sert de sceau : tant qu'il n'a pas changé, ce qu'on a
	// déjà lu vaut toujours, et le fichier n'est pas retéléchargé. Une lecture
	// courante coûte donc une seule requête.
	if garde, connu := s.kept(); connu && garde.Head == head {
		garde.Seeded = true
		return garde, nil
	}
	// Les deux fichiers sont lus au même commit : ce qu'on apprend des
	// utilisateurs et ce qu'on apprend des échéances doit décrire le même
	// instant. Un fichier absent n'est pas une panne — une organisation où
	// l'on n'a jamais daté de travail n'a pas de « travaux.json ».
	utilisateurs, err := s.client.ReadFile(s.org, RepoName, UsersFile, head)
	if err != nil {
		return Snapshot{}, s.step("lecture du fichier "+UsersFile, err)
	}
	travaux, err := s.client.ReadFile(s.org, RepoName, AssignmentsFile, head)
	if err != nil {
		return Snapshot{}, s.step("lecture du fichier "+AssignmentsFile, err)
	}
	declarees, err := s.client.ReadFile(s.org, RepoName, RulesFile, head)
	if err != nil {
		return Snapshot{}, s.step("lecture du fichier "+RulesFile, err)
	}
	enseignements, err := s.client.ReadFile(s.org, RepoName, CatalogFile, head)
	if err != nil {
		return Snapshot{}, s.step("lecture du fichier "+CatalogFile, err)
	}
	marquees, err := s.client.ReadFile(s.org, RepoName, MarksFile, head)
	if err != nil {
		return Snapshot{}, s.step("lecture du fichier "+MarksFile, err)
	}
	demandees, err := s.client.ReadFile(s.org, RepoName, AsksFile, head)
	if err != nil {
		return Snapshot{}, s.step("lecture du fichier "+AsksFile, err)
	}

	var soucis []string
	var fiches []User
	var dates []Assignment
	var regles rules.Rules
	var catalogue exchange.Catalog
	var marques signature.Book
	var demandes exchange.Asks
	if utilisateurs != nil {
		lu, ennuis := Decode(utilisateurs.Content)
		fiches, soucis = lu.All(), append(soucis, ennuis...)
	}
	if travaux != nil {
		lues, ennuis := DecodeAssignments(travaux.Content)
		dates, soucis = lues, append(soucis, ennuis...)
	}
	if declarees != nil {
		lues, ennuis := rules.Decode(declarees.Content)
		regles, soucis = lues, append(soucis, ennuis...)
	}
	if enseignements != nil {
		lu, ennuis := exchange.DecodeCatalog(enseignements.Content)
		catalogue, soucis = lu, append(soucis, ennuis...)
	}
	if marquees != nil {
		lu, ennuis := signature.DecodeBook(marquees.Content)
		marques, soucis = lu, append(soucis, ennuis...)
	}
	if demandees != nil {
		lues, ennuis := exchange.DecodeAsks(demandees.Content)
		demandes, soucis = lues, append(soucis, ennuis...)
	}
	set := newSet(fiches, dates, regles, catalogue, marques, demandes)
	s.keep(head, set, soucis)
	return Snapshot{Set: set, Head: head, Issues: soucis, Seeded: utilisateurs != nil}, nil
}

// kept relit ce que le disque retient du registre.
func (s *Store) kept() (Snapshot, bool) {
	if s.local == nil {
		return Snapshot{}, false
	}
	var garde keptSet
	if !s.local.Get(cache.RegistryKey(s.org), cache.RegistryTTL, &garde) ||
		garde.Head == "" || garde.Schema != keptSchema {
		return Snapshot{}, false
	}
	return Snapshot{
		Set: newSet(garde.Users, garde.Assignments, garde.Rules, garde.Catalog,
			garde.Marks, garde.Asks),
		Head: garde.Head, Issues: garde.Issues,
	}, true
}

// keep scelle sur le disque ce qu'on vient de lire, avis compris : ce que le
// fichier a de fâcheux ne disparaît pas parce qu'on ne le retélécharge plus.
func (s *Store) keep(head string, set *Set, soucis []string) {
	if s.local == nil || head == "" {
		return
	}
	s.local.Set(cache.RegistryKey(s.org), keptSet{
		Schema: keptSchema, Head: head, Issues: soucis,
		Users: set.All(), Assignments: set.Assignments(), Rules: set.Rules(),
		Catalog: set.Catalog(), Marks: set.Marks(), Asks: set.Asks(),
	})
}

// Apply applique un changement et rend le registre tel qu'il devient.
//
// Un changement qui ne change rien n'écrit rien : un commit sans effet salit
// l'historique sans rien apprendre à personne.
func (s *Store) Apply(change Change) (*Set, error) {
	if change.Empty() {
		snapshot, err := s.load(false)
		return snapshot.Set, err
	}
	s.mutex.Lock()
	defer s.mutex.Unlock()

	var dernier error
	for attempt := 1; attempt <= Attempts; attempt++ {
		snapshot, err := s.load(false)
		if err != nil {
			return nil, err
		}
		suivant, bouge, err := snapshot.Set.With(change, s.today())
		if err != nil {
			return nil, err
		}
		if !bouge {
			return snapshot.Set, nil
		}
		agi, err := s.ensureRepo(snapshot.Head)
		if err != nil {
			return nil, err
		}
		if agi {
			// Le dépôt vient de naître, ou d'être amorcé : la tête relevée
			// avant ne vaut plus. On recommence, c'est tout.
			continue
		}
		commit, err := s.commit(suivant, snapshot, change.message())
		if err == nil {
			// Ce qu'on vient d'écrire est de notre main : rien à signaler.
			s.keep(commit, suivant, nil)
			s.remember(Snapshot{Set: suivant, Head: commit, Seeded: true})
			return suivant, nil
		}
		if !errors.Is(err, ghapi.ErrNotFastForward) {
			return nil, err
		}
		dernier = err
	}
	return nil, valid.Errorf(
		"Registre de « %s » : %d tentatives d'écriture refusées coup sur coup. "+
			"Quelqu'un écrit sans arrêt, ou la branche « %s » a été forcée ailleurs (%v).",
		s.org, Attempts, Branch, dernier)
}

// today rend la date du jour, telle que le registre l'écrit.
func (s *Store) today() string { return s.now().Format("2006-01-02") }

// commit écrit le registre dans un commit qui descend de la tête relevée. Une
// tête vide crée la branche — c'est l'amorçage, et aussi le cas d'un
// « .cohorte » créé sans jamais avoir été rempli.
//
// Ce qui explique le dépôt n'est déposé qu'une fois, avec le registre lui-même,
// et la condition porte sur le fichier plutôt que sur la tête : un dépôt qui a
// des commits mais pas encore de registre mérite son explication autant qu'un
// dépôt neuf.
func (s *Store) commit(set *Set, snapshot Snapshot, message string) (string, error) {
	fichiers := make([]ghapi.PushFile, 0, 3)

	// Seul ce qui a bougé est réécrit. « PushFilesOnto » superpose sur l'arbre
	// du parent : un fichier qu'on ne lui donne pas reste tel quel, et
	// l'historique du registre dit alors ce qui a changé plutôt que ce qui a
	// été touché — c'est sur github.com qu'on viendra le relire.
	utilisateurs, err := set.Encode()
	if err != nil {
		return "", err
	}
	precedents, err := snapshot.Set.Encode()
	if err != nil {
		return "", err
	}
	// Un registre pas encore amorcé reçoit son fichier même vide : c'est lui
	// qui fait du dépôt un registre, et le README qui l'accompagne l'explique.
	if !snapshot.Seeded || !bytes.Equal(utilisateurs, precedents) {
		fichiers = append(fichiers, ghapi.PushFile{
			Path: UsersFile, Mode: "100644", Content: utilisateurs})
	}

	travaux, err := encodeAssignments(set.Assignments())
	if err != nil {
		return "", err
	}
	anciens, err := encodeAssignments(snapshot.Set.Assignments())
	if err != nil {
		return "", err
	}
	if !bytes.Equal(travaux, anciens) {
		fichiers = append(fichiers, ghapi.PushFile{
			Path: AssignmentsFile, Mode: "100644", Content: travaux})
	}

	// Les règles ne sont écrites que lorsqu'elles disent quelque chose : une
	// organisation qui n'en déclare aucune n'a pas de « regles.json », et
	// déposer un fichier vide ferait croire qu'on en a perdu.
	declarees, err := rules.Encode(set.Rules())
	if err != nil {
		return "", err
	}
	precedentes, err := rules.Encode(snapshot.Set.Rules())
	if err != nil {
		return "", err
	}
	if !set.Rules().Empty() && !bytes.Equal(declarees, precedentes) {
		fichiers = append(fichiers, ghapi.PushFile{
			Path: RulesFile, Mode: "100644", Content: declarees})
	}

	catalogue, err := exchange.EncodeCatalog(set.Catalog())
	if err != nil {
		return "", err
	}
	ancienCatalogue, err := exchange.EncodeCatalog(snapshot.Set.Catalog())
	if err != nil {
		return "", err
	}
	if !set.Catalog().Empty() && !bytes.Equal(catalogue, ancienCatalogue) {
		fichiers = append(fichiers, ghapi.PushFile{
			Path: CatalogFile, Mode: "100644", Content: catalogue})
	}

	marques, err := signature.EncodeBook(set.Marks())
	if err != nil {
		return "", err
	}
	anciennesMarques, err := signature.EncodeBook(snapshot.Set.Marks())
	if err != nil {
		return "", err
	}
	if !set.Marks().Empty() && !bytes.Equal(marques, anciennesMarques) {
		fichiers = append(fichiers, ghapi.PushFile{
			Path: MarksFile, Mode: "100644", Content: marques})
	}

	demandes, err := exchange.EncodeAsks(set.Asks())
	if err != nil {
		return "", err
	}
	anciennesDemandes, err := exchange.EncodeAsks(snapshot.Set.Asks())
	if err != nil {
		return "", err
	}
	if !set.Asks().Empty() && !bytes.Equal(demandes, anciennesDemandes) {
		fichiers = append(fichiers, ghapi.PushFile{
			Path: AsksFile, Mode: "100644", Content: demandes})
	}

	if !snapshot.Seeded {
		fichiers = append(fichiers, ghapi.PushFile{
			Path: ReadmeFile, Mode: "100644", Content: Readme(s.org)})
	}
	// Un changement peut s'annuler lui-même — fixer puis retirer la même date
	// d'un seul coup. Il n'y a alors rien à écrire, et un commit dont l'arbre
	// est celui de son parent ne dirait rien à personne.
	if len(fichiers) == 0 {
		return snapshot.Head, nil
	}
	commit, err := s.client.PushFilesOnto(
		s.org, RepoName, fichiers, message, Branch, snapshot.Head)
	if err != nil && !errors.Is(err, ghapi.ErrNotFastForward) {
		// Le refus d'avance rapide n'est pas une panne : il est attendu, et la
		// boucle d'écriture le reconnaît. L'enrober le rendrait méconnaissable
		// pour elle — et de toute façon, il n'a rien à expliquer.
		return "", s.step("écriture", err)
	}
	return commit, err
}

// ensureRepo s'assure que le dépôt du registre existe, qu'il est privé, et
// qu'il porte au moins un commit. Le booléen dit qu'il a fallu agir : ce qu'on
// avait relevé de la branche ne vaut alors plus, et l'appelant doit relire.
//
// Un dépôt sans aucun commit n'est pas encore un dépôt git pour GitHub, dont
// l'API Git y répond « Git Repository is empty. ». Deux chemins y mènent, et
// tous deux se rencontrent : un dépôt qu'on crée, et un dépôt déjà là mais
// jamais rempli — par une écriture interrompue, ou fait à la main.
//
// Le premier naît garni : « auto_init » lui donne son commit initial. Le
// second s'amorce par l'API des contenus, la seule qui sache écrire dans un
// dépôt que l'API Git refuse encore.
//
// Le registre porte des noms d'étudiants. Il est créé privé, et l'outil refuse
// d'y écrire s'il a été rendu public : mieux vaut une écriture qui échoue
// bruyamment qu'une liste de noms exposée sans que personne s'en aperçoive.
func (s *Store) ensureRepo(head string) (bool, error) {
	repo, err := s.client.GetRepo(s.org, RepoName)
	if err != nil {
		return false, s.step("ouverture", err)
	}
	if repo == nil {
		if _, err := s.client.CreateOrgRepo(
			s.org, RepoName, true, Description, true); err != nil {
			return false, s.step("création", err)
		}
		return true, nil
	}
	if !repo.Private {
		return false, valid.Errorf(
			"Le dépôt « %s/%s » est public : il porte des noms d'étudiants et rien n'y "+
				"sera écrit tant qu'il le restera. Repassez-le en privé dans ses réglages "+
				"GitHub.", s.org, RepoName)
	}
	if head != "" {
		return false, nil
	}
	if _, err := s.client.PutFile(s.org, RepoName, ReadmeFile, Branch,
		"Explique ce dépôt", Readme(s.org)); err != nil {
		// Quelqu'un l'a amorcé pendant qu'on s'y préparait : relire suffit.
		if errors.Is(err, ghapi.ErrNotFastForward) {
			return true, nil
		}
		return false, s.step("amorçage", err)
	}
	return true, nil
}

// ForgetHistory réécrit la branche du registre en un seul commit, sans passé.
//
// Un étudiant retiré du registre reste dans l'historique : c'est ce que git
// est. Cette opération repart d'un commit orphelin portant l'état courant, et
// la branche n'a plus rien derrière elle.
//
// Ce n'est pas un effacement au sens fort, et il ne faut pas le présenter
// comme tel. Les objets devenus inaccessibles restent un temps chez GitHub
// avant d'être ramassés, et un clone déjà fait garde tout ce qu'il avait. Ce
// qui est vrai, et rien de plus : plus rien de l'ancien n'est atteignable
// depuis la branche.
func (s *Store) ForgetHistory() (string, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	head, err := s.client.BranchHead(s.org, RepoName, Branch)
	if err != nil {
		return "", s.step("lecture de la branche", err)
	}
	if head == "" {
		return "", valid.Errorf(
			"Le registre de « %s » n'a rien écrit : il n'y a pas d'historique à effacer.", s.org)
	}
	tree, err := s.client.CommitTree(s.org, RepoName, head)
	if err != nil {
		return "", s.step("lecture du commit en place", err)
	}
	orphelin, err := s.client.CreateCommit(s.org, RepoName,
		"Repart du registre courant, sans son historique", tree, nil)
	if err != nil {
		return "", s.step("écriture du commit orphelin", err)
	}
	if err := s.client.ResetBranchHead(s.org, RepoName, Branch, orphelin); err != nil {
		return "", s.step("réécriture de la branche", err)
	}
	// Le sceau change : ce qu'on retenait du registre ne vaut plus.
	if s.local != nil {
		s.local.Forget(cache.RegistryKey(s.org))
	}
	s.unremember()
	return orphelin, nil
}

// ------------------------------------------------------------ confidentialité

// Exposure dit qui, dans l'organisation, peut lire le registre sans qu'on le
// lui ait donné.
//
// Les étudiants sont d'ordinaire collaborateurs externes de leur seul dépôt :
// ils ne voient rien du registre. Mais une organisation peut accorder d'office
// un droit de lecture à tous ses membres, et un département qui inscrit ses
// étudiants comme membres leur ouvrirait alors la liste de leurs camarades.
//
// GitHub ne montre ce réglage qu'aux propriétaires. Une chaîne vide veut donc
// dire « on ne sait pas », et l'absence de réponse n'est pas un feu vert : elle
// ne dit rien, et l'appelant doit le présenter ainsi.
// Teams énumère les équipes de l'organisation, pour qu'on puisse en désigner
// une. Un compte qui n'est pas membre n'en voit aucune : ce n'est pas une
// panne, il n'y a simplement rien à proposer.
func (s *Store) Teams() ([]ghapi.Team, error) {
	equipes, err := s.client.ListOrgTeams(s.org)
	if err != nil {
		return nil, fmt.Errorf("lecture des équipes de « %s » : %w", s.org, err)
	}
	return equipes, nil
}

// Grant donne à une équipe un droit de lecture sur le registre.
//
// Cela ouvre un accès ; cela n'en ferme aucun. Ce qui restreint réellement le
// registre, c'est la permission de base de l'organisation — d'où l'intérêt de
// la mettre à « none » et de nommer ici l'équipe enseignante, plutôt que de
// croire que désigner une équipe suffit. « Exposure » le dit à sa façon.
func (s *Store) Grant(team string) error {
	if strings.TrimSpace(team) == "" {
		return valid.Errorf("Aucune équipe désignée.")
	}
	// Le registre n'est pas lu ici : seul l'état du dépôt compte, et un dépôt
	// vide s'amorce avant qu'on n'y attache une équipe.
	snapshot, err := s.load(false)
	if err != nil {
		return err
	}
	if _, err := s.ensureRepo(snapshot.Head); err != nil {
		return err
	}
	return s.step("accès de l'équipe « "+team+" »",
		s.client.GrantTeamRepo(s.org, team, s.org, RepoName, TeamPermission))
}

// TeamPermission est le droit accordé à l'équipe enseignante : lire et écrire.
// Le registre se corrige à plusieurs, et le limiter à la lecture obligerait à
// passer par une seule personne.
const TeamPermission = "push"

func (s *Store) Exposure() string {
	// L'organisation est demandée sous la forme qui tolère un refus : ne pas
	// pouvoir lire ce réglage n'est pas une panne, c'est une ignorance.
	org, err := s.client.GetRepoOwnerOrg(s.org)
	if err != nil || org == nil {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(org.DefaultRepositoryPermission)) {
	case "read", "write", "admin":
		return fmt.Sprintf(
			"Tout membre de « %s » a d'office un droit « %s » sur ses dépôts : si vos "+
				"étudiants y sont membres, ils peuvent lire le registre. Restreignez "+
				"« %s » à l'équipe enseignante, ou passez la permission de base à « none ».",
			s.org, org.DefaultRepositoryPermission, RepoName)
	}
	return ""
}
