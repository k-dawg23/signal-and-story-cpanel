function escapeHtml(input: string) {
  return input
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll("\"", "&quot;")
    .replaceAll("'", "&#039;");
}

function unescapeNewlines(input: string) {
  // Our seed SQL used literal "\n" sequences; normalize for display.
  return input.replaceAll("\\n", "\n");
}

export function renderProductDescription(descriptionMd: string): string {
  const normalized = unescapeNewlines(descriptionMd);
  const lines = normalized.split("\n");

  const blocks: string[] = [];
  let list: string[] = [];
  let para: string[] = [];

  const flushPara = () => {
    if (!para.length) return;
    blocks.push(`<p>${inlineMarkdown(para.join(" ").trim())}</p>`);
    para = [];
  };
  const flushList = () => {
    if (!list.length) return;
    blocks.push(`<ul>${list.map((li) => `<li>${inlineMarkdown(li)}</li>`).join("")}</ul>`);
    list = [];
  };

  for (const raw of lines) {
    const line = raw.trimEnd();
    if (line.trim() === "") {
      flushList();
      flushPara();
      continue;
    }

    if (line.startsWith("- ")) {
      flushPara();
      list.push(escapeHtml(line.slice(2).trim()));
      continue;
    }

    flushList();
    para.push(escapeHtml(line.trim()));
  }

  flushList();
  flushPara();

  return blocks.join("\n");
}

function inlineMarkdown(escaped: string): string {
  // Minimal markdown: **bold** only (content already HTML-escaped).
  return escaped.replaceAll(/\*\*(.+?)\*\*/g, "<strong>$1</strong>");
}

