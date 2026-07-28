/**
 * Every doc in /docs opens with an `# H1` that repeats its frontmatter
 * title — correct when the file is read on GitHub, a duplicate heading
 * once Starlight renders the title itself. This drops that first H1 (and
 * only that one) so the same file reads right in both places.
 */
export function stripTitleHeading() {
  return (tree) => {
    const first = tree.children.findIndex(
      (node) => node.type !== "yaml" && node.type !== "toml",
    );
    if (first === -1) return;
    const node = tree.children[first];
    if (node.type === "heading" && node.depth === 1) {
      tree.children.splice(first, 1);
    }
  };
}
