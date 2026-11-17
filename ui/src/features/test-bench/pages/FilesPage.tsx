import { useEffect, useState } from "react";
import { Alert, Panel } from "../../../components/kit";
import { rpcTyped, toRpcError } from "../../../lib/bridge";
import type { RpcMethods } from "../../../proto/contract";
import { LoopFiles } from "../../loops/components/LoopFiles";

type Loop = RpcMethods["loop.list"]["result"]["loops"][number];

export function FilesPage() {
  const [loops, setLoops] = useState<Loop[]>([]);
  const [id, setId] = useState("");
  const [error, setError] = useState("");
  useEffect(() => {
    let active = true;
    void rpcTyped("loop.list", { all: true }).then((result) => {
      if (active) { setLoops(result.loops); setId((current) => current || result.loops[0]?.id || ""); }
    }).catch((cause) => { if (active) setError(toRpcError(cause).message); });
    return () => { active = false; };
  }, []);
  const loop = loops.find((item) => item.id === id);
  return <div className="space-y-4" data-testid="bench-files">
    <Panel tone="slate"><h2 className="text-lg font-semibold">Loop file reader</h2>
      <p className="mt-1 text-sm">Exercise the same daemon file and document calls used by the product loop view.</p>
      <label className="mt-3 block text-sm">Recorded loop
        <select className="mt-1 w-full rounded-lg border border-slate-300 bg-white p-2 text-slate-900" value={id} onChange={(event) => setId(event.target.value)}>
          {loops.map((item) => <option key={item.id} value={item.id}>{item.componentRoot} · {item.title || item.slug}</option>)}
        </select></label>
      {loops.length === 0 && <p className="mt-2 text-sm">Start a loop before testing its files.</p>}
    </Panel>
    {error && <Alert tone="red" title="Could not list loops" description={error} />}
    {loop && <LoopFiles key={loop.id} id={loop.id} slug={loop.slug} root={loop.componentRoot} produced={[]}
      live={{ state: "current", endpoint: "test bench", daemonVersion: "", protocolVersion: 1, generation: 1 }} />}
  </div>;
}
