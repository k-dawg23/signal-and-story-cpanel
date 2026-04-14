-- Seed a starter catalog (premium, universe-inspired).
-- Safe to re-run: products upsert on handle; mappings are rebuilt.

WITH upsert_products AS (
  INSERT INTO products (handle, name, description_md, price_cents, currency, featured_rank, active, image_url)
  VALUES
    ('neon-wyrm-patch-cable', 'Neon Wyrm Patch Cable', 'A low-capacitance patch cable with **dragon-scale shielding** and a subtle neon glow. Built for silent signal paths in crowded neon alleys.\n\n- Length: 15cm\n- Connectors: low-profile right-angle\n- Universe: Cyberpunk bazaar', 1299, 'GBP', 10, true, '/images/products/neon-wyrm-patch-cable.png'),
    ('voidglass-instrument-cable-3m', 'Voidglass Instrument Cable (3m)', 'A stage cable forged from **voidglass polymer**—flexible, tough, and quieter than a cathedral at midnight.\n\n- Length: 3m\n- Shielding: braided + foil\n- Universe: Dark Fantasy', 2499, 'GBP', 12, true, '/images/products/voidglass-instrument-cable-3m.png'),
    ('starlance-instrument-cable-6m', 'Starlance Instrument Cable (6m)', 'Long runs without the loss. A **starlit conductor** optimized for clarity—made for arena decks and starship holds.\n\n- Length: 6m\n- Universe: Sci‑Fi', 3499, 'GBP', 13, true, '/images/products/starlance-instrument-cable-6m.png'),

    ('grimwood-tuner', 'Grimwood Clip Tuner', 'A pocket oracle for your headstock—fast tracking, bright display, and a runic silhouette.\n\n- Modes: guitar/bass/chromatic\n- Battery: CR2032\n- Universe: Fantasy', 1699, 'GBP', 9, true, '/images/products/grimwood-tuner.png'),
    ('phasegate-metronome', 'Phasegate Pocket Metronome', 'A metronome that clicks like a **portal stabilizer**. Tap tempo, subdivisions, and silent vibration mode.\n\n- Vibration mode: yes\n- Subdivisions: 1/4–1/32\n- Universe: Sci‑Fi', 2299, 'GBP', 8, true, '/images/products/phasegate-metronome.png'),
    ('runegrid-practice-pad', 'Runegrid Practice Pad', 'Practice with purpose. A dense rubber pad printed with a **rune grid** to map sticking patterns and rhythmic spells.\n\n- Size: 10\" surface\n- Includes: pattern guide\n- Universe: Fantasy', 1999, 'GBP', 7, true, '/images/products/runegrid-practice-pad.png'),

    ('chrome-cathedral-overdrive', 'Chrome Cathedral Overdrive', 'A boutique overdrive voiced for **cathedral-sized chords** and razor harmonics. Responsive, touchy, and premium.\n\n- Controls: gain/level/tone\n- True bypass\n- Universe: Cyberpunk', 15999, 'GBP', 1, true, '/images/products/chrome-cathedral-overdrive.png'),
    ('lichlight-delay', 'Lichlight Delay', 'Warm repeats with a haunting tail. From subtle ambience to **necromancer swells**.\n\n- Time: up to 800ms\n- Modulation: subtle drift\n- Universe: Dark Fantasy', 17999, 'GBP', 2, true, '/images/products/lichlight-delay.png'),
    ('starforged-reverb', 'Starforged Reverb', 'A reverb engine inspired by vacuum chambers and lunar halls. **Shimmer** that stays musical.\n\n- Modes: hall/plate/shimmer\n- Trails: on/off\n- Universe: Sci‑Fi', 18999, 'GBP', 3, true, '/images/products/starforged-reverb.png'),
    ('ironfable-chorus', 'Ironfable Chorus', 'Chorus that reads like an epic. Width without warble; **legendary** on clean and driven tones.\n\n- Depth/rate/mix\n- Universe: Fantasy', 16999, 'GBP', 4, true, '/images/products/ironfable-chorus.png'),
    ('glitch-hex-fuzz', 'Glitch Hex Fuzz', 'A fuzz that stutters and snarls—**cursed code** in a stompbox.\n\n- Toggle: stable / corrupted\n- Universe: Cyberpunk', 19999, 'GBP', 5, true, '/images/products/glitch-hex-fuzz.png'),
    ('duskritual-compressor', 'Duskritual Compressor', 'Smooth sustain for lead lines and tight funk. A compressor that feels like **armor**.\n\n- Blend control\n- Universe: Dark Fantasy', 14999, 'GBP', 6, true, '/images/products/duskritual-compressor.png'),

    ('astral-straplocks', 'Astral Straplocks', 'Lock in. A machined set of straplocks with **stellar engraving**.\n\n- Pair\n- Includes: felt washers\n- Universe: Sci‑Fi', 1899, 'GBP', 14, true, '/images/products/astral-straplocks.png'),
    ('forgeweld-string-winder', 'Forgeweld String Winder', 'A compact winder with a steel core and soft grip—built like a **dwarven tool**.\n\n- Fits: most tuners\n- Universe: Fantasy', 1099, 'GBP', 15, true, '/images/products/forgeweld-string-winder.png'),
    ('nightmarket-pick-tin', 'Nightmarket Pick Tin', 'Twelve premium picks in a **nightmarket** collectible tin.\n\n- Gauge: mixed\n- Finish: matte\n- Universe: Cyberpunk', 999, 'GBP', 16, true, '/images/products/nightmarket-pick-tin.png'),

    ('sigil-pedalboard-tape', 'Sigil Pedalboard Tape', 'High-grip hook & loop with **sigil markings** to align your rig like a spell diagram.\n\n- Length: 2m roll\n- Universe: Fantasy', 899, 'GBP', 17, true, '/images/products/sigil-pedalboard-tape.png'),
    ('ionforge-pedal-power-daisy', 'Ionforge Power Daisy Chain', 'Low-noise power daisy chain for compact rigs. The **ionforge** build resists stage abuse.\n\n- Plugs: 8\n- Universe: Sci‑Fi', 1399, 'GBP', 18, true, '/images/products/ionforge-pedal-power-daisy.png'),
    ('blackspire-knob-set', 'Blackspire Knob Set', 'A set of anodized knobs—sharp, minimal, and premium. For rigs that look like they came from a **blackspire forge**.\n\n- Pack: 4\n- Universe: Dark Fantasy', 1599, 'GBP', 19, true, '/images/products/blackspire-knob-set.png'),

    ('signal-story-hoodie', 'Signal & Story Hoodie', 'Heavyweight hoodie with a subtle glow print: **Where Sound Meets Story**.\n\n- Fit: relaxed\n- Universe: cross‑realm', 5999, 'GBP', 20, true, '/images/products/signal-story-hoodie.png'),
    ('runegrid-poster-set', 'Runegrid Poster Set', 'Two A2 prints—one neon grid, one rune parchment. For studios that feel like a **portal room**.\n\n- Size: A2\n- Pack: 2\n- Universe: mixed', 2499, 'GBP', 21, true, '/images/products/runegrid-poster-set.png')
  ON CONFLICT (handle) DO UPDATE SET
    name = EXCLUDED.name,
    description_md = replace(EXCLUDED.description_md, '\\n', E'\n'),
    price_cents = EXCLUDED.price_cents,
    currency = EXCLUDED.currency,
    featured_rank = EXCLUDED.featured_rank,
    active = EXCLUDED.active,
    image_url = EXCLUDED.image_url
  RETURNING id, handle
),
collections_map AS (
  SELECT id, type, slug FROM collections
),
delete_existing AS (
  DELETE FROM product_collections
)
INSERT INTO product_collections (product_id, collection_id)
SELECT p.id, c.id
FROM upsert_products p
CROSS JOIN LATERAL (
  VALUES
    -- Cables
    (CASE WHEN p.handle IN ('neon-wyrm-patch-cable','voidglass-instrument-cable-3m','starlance-instrument-cable-6m') THEN 'cables' END, 'category'),
    -- Practice tools
    (CASE WHEN p.handle IN ('grimwood-tuner','phasegate-metronome','runegrid-practice-pad') THEN 'practice-tools' END, 'category'),
    -- Pedals
    (CASE WHEN p.handle IN ('chrome-cathedral-overdrive','lichlight-delay','starforged-reverb','ironfable-chorus','glitch-hex-fuzz','duskritual-compressor') THEN 'pedals' END, 'category'),
    -- Hardware / Accessories
    (CASE WHEN p.handle IN ('astral-straplocks','blackspire-knob-set') THEN 'hardware' END, 'category'),
    (CASE WHEN p.handle IN ('forgeweld-string-winder','nightmarket-pick-tin','sigil-pedalboard-tape','ionforge-pedal-power-daisy') THEN 'accessories' END, 'category'),
    -- Art & Lifestyle
    (CASE WHEN p.handle IN ('signal-story-hoodie','runegrid-poster-set') THEN 'art-lifestyle' END, 'category'),

    -- Themes
    (CASE WHEN p.handle IN ('neon-wyrm-patch-cable','chrome-cathedral-overdrive','glitch-hex-fuzz','nightmarket-pick-tin') THEN 'cyberpunk' END, 'theme'),
    (CASE WHEN p.handle IN ('grimwood-tuner','runegrid-practice-pad','ironfable-chorus','sigil-pedalboard-tape','forgeweld-string-winder') THEN 'fantasy' END, 'theme'),
    (CASE WHEN p.handle IN ('starlance-instrument-cable-6m','phasegate-metronome','starforged-reverb','astral-straplocks','ionforge-pedal-power-daisy') THEN 'sci-fi' END, 'theme'),
    (CASE WHEN p.handle IN ('voidglass-instrument-cable-3m','lichlight-delay','duskritual-compressor','blackspire-knob-set') THEN 'dark-fantasy' END, 'theme'),

    -- Instruments (mostly general/guitar-bass)
    (CASE WHEN p.handle IN ('chrome-cathedral-overdrive','lichlight-delay','starforged-reverb','ironfable-chorus','glitch-hex-fuzz','duskritual-compressor') THEN 'guitar-bass' END, 'instrument'),
    (CASE WHEN p.handle IN ('grimwood-tuner','phasegate-metronome','runegrid-practice-pad','neon-wyrm-patch-cable','voidglass-instrument-cable-3m','starlance-instrument-cable-6m') THEN 'general' END, 'instrument')
) AS tags(slug, type)
JOIN collections c ON c.slug = tags.slug AND c.type::text = tags.type
WHERE tags.slug IS NOT NULL
ON CONFLICT DO NOTHING;

