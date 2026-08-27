import { HighlightStyle, syntaxHighlighting } from "@codemirror/language";
import { tags as t } from "@lezer/highlight";
import { EditorView } from "@codemirror/view";

const highlightStyle = HighlightStyle.define([
  { tag: [t.comment, t.lineComment, t.blockComment], color: "#9aa894", fontStyle: "italic" },
  { tag: [t.string, t.docString], color: "#e1b879" },
  { tag: [t.number, t.float], color: "#e0a17f" },
  { tag: [t.bool, t.null, t.atom], color: "#e38d7e" },
  { tag: [t.keyword, t.operatorKeyword, t.definitionKeyword, t.moduleKeyword], color: "#a9c4e3" },
  { tag: [t.variableName, t.definition(t.variableName), t.local(t.variableName)], color: "#f1e3cc" },
  { tag: [t.propertyName, t.labelName], color: "#d8c09b" },
  { tag: [t.typeName, t.className], color: "#c0d3ad" },
  { tag: [t.operator, t.arithmeticOperator, t.logicOperator], color: "#d0ad7e" },
  { tag: [t.meta, t.annotation], color: "#c09ac2" },
  { tag: [t.punctuation, t.brace, t.separator], color: "#b9c3c8" },
  { tag: t.link, color: "#9fc6e5", textDecoration: "underline" },
]);

export const editorTheme = [
  EditorView.theme(
    {
      "&": {
        color: "#f1e3cc",
        backgroundColor: "#101b2d",
      },
      ".cm-content": {
        caretColor: "#f0c789",
      },
      ".cm-cursor, .cm-dropCursor": {
        borderLeftColor: "#f0c789",
      },
      ".cm-gutters": {
        color: "#c3a98b",
        backgroundColor: "#2a211d",
        borderRight: "1px solid #604735",
      },
      ".cm-activeLine": {
        backgroundColor: "#18263a",
      },
      ".cm-activeLineGutter": {
        color: "#f0c789",
        backgroundColor: "#3b2b23",
      },
      ".cm-selectionBackground, ::selection": {
        backgroundColor: "#765638 !important",
        color: "#fff4df",
      },
      ".cm-matchingBracket": {
        backgroundColor: "#59422d",
        outline: "1px solid #c6975e",
      },
      ".cm-tooltip": {
        color: "#f1e3cc",
        backgroundColor: "#2a211d",
        border: "1px solid #604735",
      },
    },
    { dark: true },
  ),
  syntaxHighlighting(highlightStyle),
];
