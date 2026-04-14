export const categoryCollections = [
  { slug: "accessories", name: "Accessories" },
  { slug: "pedals", name: "Pedals" },
  { slug: "practice-tools", name: "Practice Tools" },
  { slug: "cables", name: "Cables" },
  { slug: "art-lifestyle", name: "Art & Lifestyle" },
  { slug: "hardware", name: "Hardware" },
];

export const themeCollections = [
  { slug: "cyberpunk", name: "Cyberpunk" },
  { slug: "fantasy", name: "Fantasy" },
  { slug: "sci-fi", name: "Sci-Fi" },
  { slug: "dark-fantasy", name: "Dark Fantasy" },
];

export const instrumentCollections = [
  { slug: "guitar", name: "Guitar" },
  { slug: "bass", name: "Bass" },
  { slug: "guitar-bass", name: "Guitar/Bass" },
  { slug: "general", name: "General" },
];

export const allCollections = [
  ...categoryCollections.map((c) => ({ ...c, type: "category" as const })),
  ...themeCollections.map((c) => ({ ...c, type: "theme" as const })),
  ...instrumentCollections.map((c) => ({ ...c, type: "instrument" as const })),
];

