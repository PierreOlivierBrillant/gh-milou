package valid

import "strconv"

// Un nombre s'écrit avec le mot qui s'y accorde. « 19 dépôt(s) » laisse le
// lecteur faire l'accord à la place de l'outil, et « 1 enseignant(s) » se
// remarque à chaque fois. La page suit la même règle avec « pluriel() » et
// « accord() » : un même compte doit se lire pareil dans les trois interfaces.

// Plural écrit un nombre suivi de la forme qui s'y accorde : « 1 dépôt »,
// « 3 dépôts ». Le français met le singulier jusqu'à un, zéro compris.
//
// La forme plurielle vaut le singulier suivi d'un « s », sauf quand on la
// donne. On la donne pour un mot qui ne prend pas de « s » — « index » —, et
// pour une phrase entière, où l'accord touche plus que le nom : « dépôt n'a
// pas pu être lu », « dépôts n'ont pas pu être lus ».
func Plural(count int, singular string, plural ...string) string {
	return strconv.Itoa(count) + " " + Agree(count, singular, plural...)
}

// Agree rend seulement la forme qui convient à un nombre écrit ailleurs dans la
// phrase : un participe, un verbe — « 3 dépôts sur 5 ont été renommés ».
func Agree(count int, singular string, plural ...string) string {
	if count < 2 && count > -2 {
		return singular
	}
	if len(plural) > 0 {
		return plural[0]
	}
	return singular + "s"
}
