package registry

import "time"

// Avancer fait passer le temps sur un magasin. Ce qu'un collègue écrit ailleurs
// ne se voit qu'une fois « Recheck » écoulé : les tests qui jouent un autre
// poste, ou une panne, doivent laisser passer cette minute.
func (s *Store) Avancer(duree time.Duration) {
	avant := s.now
	s.now = func() time.Time { return avant().Add(duree) }
}
