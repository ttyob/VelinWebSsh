<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { basicSetup } from "codemirror";
import { json } from "@codemirror/lang-json";
import { StreamLanguage } from "@codemirror/language";
import { yaml } from "@codemirror/legacy-modes/mode/yaml";
import { EditorState } from "@codemirror/state";
import { EditorView, keymap } from "@codemirror/view";
import { defaultKeymap, historyKeymap } from "@codemirror/commands";
import { searchKeymap } from "@codemirror/search";
import { editorTheme } from "../editorTheme";

const props = withDefaults(
  defineProps<{
    modelValue: string;
    language?: "json" | "yaml";
    height?: string;
  }>(),
  { language: "yaml", height: "min(62vh, 620px)" },
);
const emit = defineEmits<{ "update:modelValue": [value: string] }>();

const mount = ref<HTMLElement>();
let editor: EditorView | undefined;

function languageExtension() {
  return props.language === "json" ? json() : StreamLanguage.define(yaml);
}

function destroyEditor() {
  editor?.destroy();
  editor = undefined;
}

async function createEditor() {
  await nextTick();
  if (!mount.value) return;
  destroyEditor();
  editor = new EditorView({
    parent: mount.value,
    state: EditorState.create({
      doc: props.modelValue,
      extensions: [
        basicSetup,
        keymap.of([...defaultKeymap, ...historyKeymap, ...searchKeymap]),
        languageExtension(),
        editorTheme,
        EditorView.lineWrapping,
        EditorView.updateListener.of((update) => {
          if (update.docChanged) emit("update:modelValue", update.state.doc.toString());
        }),
      ],
    }),
  });
}

watch(
  () => props.modelValue,
  (value) => {
    if (!editor || value === editor.state.doc.toString()) return;
    editor.dispatch({
      changes: { from: 0, to: editor.state.doc.length, insert: value },
    });
  },
);
watch(() => props.language, createEditor);
onMounted(createEditor);
onBeforeUnmount(destroyEditor);
</script>

<template>
  <div class="code-editor" :style="{ height }">
    <div ref="mount" class="code-editor-mount" />
  </div>
</template>

<style scoped>
.code-editor {
  min-height: 280px;
  overflow: hidden;
  border: 1px solid #604735;
  background: #101b2d;
}
.code-editor-mount,
.code-editor-mount :deep(.cm-editor) {
  height: 100%;
}
.code-editor-mount :deep(.cm-editor) {
  color: #f1e3cc;
  background: #101b2d;
  font-family: "JetBrains Mono", "Cascadia Code", Consolas, monospace;
  font-size: 13px;
}
.code-editor-mount :deep(.cm-scroller) {
  overflow: auto;
  line-height: 1.65;
}
.code-editor-mount :deep(.cm-gutters) {
  color: #c3a98b;
  border-right-color: #604735;
  background: #2a211d;
}
.code-editor-mount :deep(.cm-activeLine),
.code-editor-mount :deep(.cm-activeLineGutter) {
  background: #18263a;
}
.code-editor-mount :deep(.cm-selectionBackground),
.code-editor-mount :deep(.cm-content ::selection) {
  background: #765638 !important;
}
.code-editor-mount :deep(.cm-focused) {
  outline: none;
}
</style>
