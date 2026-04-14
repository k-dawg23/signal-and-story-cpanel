INSERT INTO collections (type, slug, name)
VALUES
  ('category', 'accessories', 'Accessories'),
  ('category', 'pedals', 'Pedals'),
  ('category', 'practice-tools', 'Practice Tools'),
  ('category', 'cables', 'Cables'),
  ('category', 'art-lifestyle', 'Art & Lifestyle'),
  ('category', 'hardware', 'Hardware'),
  ('theme', 'cyberpunk', 'Cyberpunk'),
  ('theme', 'fantasy', 'Fantasy'),
  ('theme', 'sci-fi', 'Sci-Fi'),
  ('theme', 'dark-fantasy', 'Dark Fantasy'),
  ('instrument', 'guitar', 'Guitar'),
  ('instrument', 'bass', 'Bass'),
  ('instrument', 'guitar-bass', 'Guitar/Bass'),
  ('instrument', 'general', 'General')
ON CONFLICT (type, slug) DO UPDATE SET
  name = EXCLUDED.name;

