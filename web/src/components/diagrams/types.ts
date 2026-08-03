// Shared shapes for the diagram components.
//
// Two authoring rules these types cannot enforce:
//
//  1. Never put `<` or `>` inside a prop string. On GitHub the component
//     tag is stripped but its attributes are parsed as HTML, and an angle
//     bracket closes the tag early. Use "→" and "·".
//  2. At most one `accent: true` per figure. The rust accent is the site's
//     single signal colour; a second one means neither is the answer.
//     Figure throws at build time if this is broken.

export interface FigureProps {
  /** Figure number as printed, e.g. "01". Omit for an unnumbered figure. */
  n?: string;
  /** Header-bar caption. Sentence case, no trailing period. */
  caption: string;
}

export interface Cell {
  /** Mono label: a package, function, type, or filename. */
  label: string;
  /** One line of prose beneath the label. */
  note?: string;
  /**
   * step     — solid edge, the default: something that runs.
   * artifact — dashed edge: something sitting on disk.
   * output   — ticked edge: something compiled out of the stage before it.
   */
  kind?: "step" | "artifact" | "output";
  /** The single rust mark. At most one per figure. */
  accent?: boolean;
}

export interface Stage {
  /** Micro-caps label above the stage. */
  title?: string;
  /** One cell is a step. Several are a fan-in, fan-out, or parallel set. */
  cells: Cell[];
  /** Mono label printed on the connector leaving this stage. */
  edge?: string;
}

export function countAccents(stages: Stage[]): number {
  return stages.reduce(
    (n, s) => n + s.cells.filter((c) => c.accent).length,
    0,
  );
}
