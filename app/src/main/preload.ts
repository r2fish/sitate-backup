// preload: レンダラへ出す窓口。主プロセスへの要求(送信・決定・提出・操作)と、描画内容の受け取りだけ。エージェントのプロセスへの経路は無い。
import { contextBridge, ipcRenderer } from "electron";
import { CHANNEL } from "./ipc-channels.ts";
import type { CardDecision, PhraseAction, UiMessage } from "./ipc-channels.ts";

contextBridge.exposeInMainWorld("sitate", {
  sendChat: (text: string): Promise<void> => ipcRenderer.invoke(CHANNEL.chatSend, text),
  abortChat: (): Promise<void> => ipcRenderer.invoke(CHANNEL.chatAbort),
  decideCard: (changeId: string, d: CardDecision): Promise<void> => ipcRenderer.invoke(CHANNEL.cardDecision, changeId, d),
  submitSecret: (ref: string, value: string): Promise<void> => ipcRenderer.invoke(CHANNEL.secretSubmit, ref, value),
  phraseAction: (a: PhraseAction): Promise<void> => ipcRenderer.invoke(CHANNEL.phraseAction, a),
  statusAction: (kind: string): Promise<void> => ipcRenderer.invoke(CHANNEL.statusAction, kind),
  registerTarget: (host: string, port: string, fingerprint: string): Promise<void> => ipcRenderer.invoke(CHANNEL.targetRegister, host, port, fingerprint),
  setFingerprint: (fp: string): Promise<void> => ipcRenderer.invoke(CHANNEL.targetFingerprint, fp),
  removeTarget: (): Promise<void> => ipcRenderer.invoke(CHANNEL.targetRemove),
  onRender: (cb: (m: UiMessage) => void): void => { ipcRenderer.on(CHANNEL.render, (_e, m: UiMessage) => cb(m)); },
});
