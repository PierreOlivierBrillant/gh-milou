package identity

import (
	"strings"

	"github.com/PierreOlivierBrillant/gh-milou/internal/cache"
	"github.com/PierreOlivierBrillant/gh-milou/internal/roster"
)

// compte est ce qu'on retient d'un compte entre deux rattachements. Un login
// vide dit « ce compte n'existe pas » : c'est une réponse aussi, et elle ne
// change pas de sitôt.
type compte struct {
	Login string `json:"login"`
	Name  string `json:"name"`
}

// Account dit si un compte GitHub existe, et sous quel nom.
//
// C'est la question qu'on pose avant de rattacher un dépôt dont le dernier
// niveau ne désigne personne. La réponse est mémorisée, absence comprise : un
// annuaire de vingt dépôts sans étudiant ne doit pas coûter vingt appels à
// chaque ouverture. Une panne, elle, ne l'est pas — elle ne dit rien du compte,
// et la retenir ferait passer un vrai compte pour un slug jusqu'à expiration.
func (r *Resolver) Account(login string) (roster.Person, bool, error) {
	demande := strings.TrimSpace(login)
	if demande == "" {
		return roster.Person{}, false, nil
	}
	cle := cache.AccountKey(demande)
	var connu compte
	if r.store.Get(cle, cache.ProfileTTL, &connu) {
		return personneDe(connu), connu.Login != "", nil
	}
	if r.client == nil {
		return roster.Person{}, false, nil
	}
	user, err := r.client.GetUser(demande)
	if err != nil {
		return roster.Person{}, false, err
	}
	if user != nil {
		connu = compte{Login: strings.TrimSpace(user.Login), Name: strings.TrimSpace(user.Name)}
		if connu.Login == "" {
			connu.Login = demande
		}
	}
	r.store.Set(cle, connu)
	// Le nom appris sert aussi à qui ne cherche que le nom : inutile de
	// redemander le profil quand on résoudra les noms d'un groupe.
	if connu.Login != "" {
		r.store.Set(cache.ProfileKey(connu.Login), connu.Name)
	}
	return personneDe(connu), connu.Login != "", nil
}

func personneDe(connu compte) roster.Person {
	return roster.Person{Username: connu.Login, FullName: connu.Name}
}
