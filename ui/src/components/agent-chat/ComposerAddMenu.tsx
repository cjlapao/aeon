import { useRef, useState } from "react";
import { Menu, type MenuItem } from "../kit";
import { AppIcon } from "../icons";

export type ChatAddAction = "files" | "folders" | "skills" | "loops" | "agents" | "commands";

export function ComposerAddMenu({ disabled, onFiles, onAction }: {
  disabled: boolean;
  onFiles: (files: File[]) => void;
  onAction?: (action: ChatAddAction) => void;
}) {
  const [open, setOpen] = useState(false);
  const filesRef = useRef<HTMLInputElement>(null);
  const foldersRef = useRef<HTMLInputElement>(null);
  const action = (id: ChatAddAction) => () => {
    setOpen(false);
    if (id === "files") filesRef.current?.click();
    if (id === "folders") foldersRef.current?.click();
    onAction?.(id);
  };
  const items: MenuItem[] = [
    { id: "add", type: "group", label: "Add", items: [
      { id: "files", label: "Files and images", icon: <AppIcon name="read" />, command: action("files") },
      { id: "folders", label: "Folder", icon: <AppIcon name="panel" />, command: action("folders") },
    ] },
    { id: "context", type: "group", label: "Context", items: [
      { id: "skills", label: "Skills and prompts", shortcut: "/", disabled: !onAction, command: action("skills") },
      { id: "loops", label: "Link another loop", shortcut: "//", disabled: !onAction, command: action("loops") },
      { id: "agents", label: "Agents", shortcut: "$", disabled: !onAction, command: action("agents") },
    ] },
    { id: "commands-group", type: "group", label: "Commands", items: [
      { id: "commands", label: "Browse commands", disabled: !onAction, command: action("commands") },
    ] },
  ];
  return <>
    <Menu items={items} open={open && !disabled} onOpenChange={setOpen} placement="top" maxHeight={280}
      width="min(400px, calc(100vw - 32px))" aria-label="Add to chat"
      trigger={<button type="button" className="chat-composer-plus" disabled={disabled} aria-label="Add to chat">+</button>} />
    <input ref={filesRef} hidden type="file" multiple aria-label="Attach files" onChange={(event) => {
      onFiles(Array.from(event.target.files ?? [])); event.target.value = "";
    }} />
    <input ref={foldersRef} hidden type="file" multiple {...{ webkitdirectory: "" }} aria-label="Attach folder" onChange={(event) => {
      onFiles(Array.from(event.target.files ?? [])); event.target.value = "";
    }} />
  </>;
}
