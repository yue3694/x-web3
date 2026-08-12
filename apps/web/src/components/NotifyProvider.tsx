import {createContext, useCallback, useContext, useMemo, useRef, useState, type ReactNode} from "react";

type NotifyTone = "error" | "success" | "info";

interface NotifyItem { id: number; tone: NotifyTone; message: string }
interface NotifyApi { notify: (message: string, tone?: NotifyTone) => void }

const NotifyContext = createContext<NotifyApi | null>(null);
const fallbackNotify: NotifyApi = {notify: () => undefined};

export function NotifyProvider({children}: {children: ReactNode}) {
    const [items, setItems] = useState<NotifyItem[]>([]);
    const nextId = useRef(1);
    const dismiss = useCallback((id: number) => setItems((current) => current.filter((item) => item.id !== id)), []);
    const notify = useCallback((message: string, tone: NotifyTone = "info") => {
        const id = nextId.current++;
        setItems((current) => [...current.slice(-3), {id, tone, message}]);
        window.setTimeout(() => dismiss(id), tone === "error" ? 6000 : 4000);
    }, [dismiss]);
    const value = useMemo(() => ({notify}), [notify]);

    return (
        <NotifyContext.Provider value={value}>
            {children}
            <aside className="notify-stack" aria-label="通知" aria-live="polite">
                {items.map((item) => (
                    <div className={`notify-toast notify-toast--${item.tone}`} role={item.tone === "error" ? "alert" : "status"} key={item.id}>
                        <span className="notify-toast__icon" aria-hidden="true">{item.tone === "success" ? "✓" : item.tone === "error" ? "!" : "i"}</span>
                        <p>{item.message}</p>
                        <button type="button" aria-label="关闭通知" onClick={() => dismiss(item.id)}>×</button>
                    </div>
                ))}
            </aside>
        </NotifyContext.Provider>
    );
}

export function useNotify(): NotifyApi {
    const value = useContext(NotifyContext);
    return value ?? fallbackNotify;
}
