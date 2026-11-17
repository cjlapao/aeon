import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { rpcTyped, toRpcError } from "../../../lib/bridge";
import type { FlowModel, FlowReport } from "./flowModel";

// The draft of one flow. The daemon owns every translation: the stored text is
// read into a structure (blueprint.model), a builder edit is written back as text
// (blueprint.render) and that text is judged (blueprint.validate). The page never
// parses or writes the flow's text, so what it shows is what the loader will read.

const DEBOUNCE_MS = 350;

export type DraftState = "clean" | "working" | "ready" | "broken";

export interface FlowDraft {
  /** The structure on screen; undefined until the stored flow has been read. */
  model: FlowModel | undefined;
  /** The text a save would send: the stored text until something changes. */
  text: string;
  dirty: boolean;
  state: DraftState;
  report: FlowReport | undefined;
  /** Why the structure could not be read or written (text that is not YAML, a refused value). */
  problem: string | undefined;
  /** Edit the structure. */
  edit: (fn: (m: FlowModel) => FlowModel) => void;
  /** Edit the text: the structure follows when the text can be read. */
  editText: (text: string) => void;
  /** Throw the draft away and go back to the stored flow. */
  discard: () => void;
  /** The stored text changed (a save landed): the draft starts again from it. */
  reset: (storedText: string) => void;
}

interface ModelAnswer { model: FlowModel }
interface RenderAnswer { content: string }

export function useFlowDraft(name: string, stored: string): FlowDraft {
  const [model, setModel] = useState<FlowModel | undefined>(undefined);
  const [baseline, setBaseline] = useState<string>("");
  const [text, setText] = useState(stored);
  const [report, setReport] = useState<FlowReport | undefined>(undefined);
  const [state, setState] = useState<DraftState>("clean");
  const [problem, setProblem] = useState<string | undefined>(undefined);
  const generation = useRef(0);
  // Set while the text drives the structure, so the structure's own re-render does not overwrite what is being typed.
  const fromText = useRef(false);

  const judge = useCallback(async (content: string, gen: number) => {
    try {
      const answer = await rpcTyped("blueprint.validate", { name, content });
      if (gen === generation.current) setReport(answer as unknown as FlowReport);
    } catch (cause) {
      if (gen === generation.current) setProblem(toRpcError(cause).message);
    }
  }, [name]);

  const openStored = useCallback(async (content: string) => {
    const gen = ++generation.current;
    setText(content);
    try {
      const answer = (await rpcTyped("blueprint.model", { content })) as unknown as ModelAnswer;
      if (gen !== generation.current) return;
      setModel(answer.model);
      setBaseline(JSON.stringify(answer.model));
      setProblem(undefined);
      setState("clean");
      void judge(content, gen);
    } catch (cause) {
      if (gen !== generation.current) return;
      setModel(undefined);
      setProblem(toRpcError(cause).message);
      setState("broken");
    }
  }, [judge]);

  // The stored flow arrived or changed: start from it.
  useEffect(() => {
    if (stored !== "") void openStored(stored);
  }, [stored, openStored]);

  // A structure edit: write it back as text, then judge that text.
  const modelKey = useMemo(() => (model ? JSON.stringify(model) : ""), [model]);
  const keyRef = useRef(modelKey);
  keyRef.current = modelKey;
  useEffect(() => {
    if (!model || modelKey === baseline) return;
    if (fromText.current) { fromText.current = false; return; }
    const gen = ++generation.current;
    setState("working");
    const timer = setTimeout(async () => {
      try {
        const answer = (await rpcTyped("blueprint.render", { model } as never)) as unknown as RenderAnswer;
        if (gen !== generation.current) return;
        setText(answer.content);
        setProblem(undefined);
        setState("ready");
        void judge(answer.content, gen);
      } catch (cause) {
        if (gen !== generation.current) return;
        setProblem(toRpcError(cause).message);
        setState("broken");
      }
    }, DEBOUNCE_MS);
    return () => clearTimeout(timer);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [modelKey]);

  // Back to the stored structure: the stored text is the draft again.
  useEffect(() => {
    if (model && modelKey === baseline && text !== stored && !fromText.current) {
      generation.current++;
      setText(stored);
      setState("clean");
      void judge(stored, generation.current);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [modelKey, baseline]);

  const edit = useCallback((fn: (m: FlowModel) => FlowModel) => setModel((current) => (current ? fn(current) : current)), []);

  const editText = useCallback((next: string) => {
    setText(next);
    const gen = ++generation.current;
    setState("working");
    void (async () => {
      try {
        const answer = (await rpcTyped("blueprint.model", { content: next })) as unknown as ModelAnswer;
        if (gen !== generation.current) return;
        fromText.current = JSON.stringify(answer.model) !== keyRef.current;
        setModel(answer.model);
        setProblem(undefined);
        setState(next === stored ? "clean" : "ready");
      } catch (cause) {
        if (gen !== generation.current) return;
        setProblem(toRpcError(cause).message);
        setState("broken");
      }
      if (gen === generation.current) void judge(next, gen);
    })();
  }, [judge, stored]);

  const discard = useCallback(() => { void openStored(stored); }, [openStored, stored]);
  const reset = useCallback((storedText: string) => { void openStored(storedText); }, [openStored]);

  return { model, text, dirty: text !== stored, state, report, problem, edit, editText, discard, reset };
}
