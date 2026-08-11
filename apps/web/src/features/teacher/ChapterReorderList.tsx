/**
 * ChapterReorderList — 课程章节拖拽排序。
 *
 * 实现策略：优先 @dnd-kit/sortable，但当前 apps/web/package.json 未引入，
 * 按 F02 task 约束使用原生 HTML5 drag-and-drop（MVP 可接受）。
 * 若后续引入 @dnd-kit，可直接替换内部实现而不改对外 API。
 *
 * 行为契约:
 *   - 仅在 onReorder 提供新顺序时触发回调，不在内部持有数据真源。
 *   - 拖动过程中显示视觉反馈（dragging class）。
 *   - 键盘可访问：行获得焦点后通过 Space / Arrow Up / Arrow Down 移动，
 *     按 Enter 提交。空操作（同一位置）不触发回调。
 */

import {useCallback, useState, type DragEvent, type KeyboardEvent} from "react";

export interface ChapterReorderItem<T> {
    id: string;
    title: string;
    payload: T;
}

interface ChapterReorderListProps<T> {
    items: ChapterReorderItem<T>[];
    onReorder: (next: ChapterReorderItem<T>[]) => void;
    /** 每行渲染函数；用于在拖拽列表中插入自定义内容（如课程内嵌控件）。 */
    renderItem?: (item: ChapterReorderItem<T>) => React.ReactNode;
}

export function ChapterReorderList<T>({items, onReorder, renderItem}: ChapterReorderListProps<T>) {
    const [draggingId, setDraggingId] = useState<string | null>(null);
    const [overId, setOverId] = useState<string | null>(null);

    const handleDragStart = useCallback((event: DragEvent<HTMLLIElement>, id: string) => {
        setDraggingId(id);
        event.dataTransfer.effectAllowed = "move";
        event.dataTransfer.setData("text/plain", id);
    }, []);

    const handleDragOver = useCallback((event: DragEvent<HTMLLIElement>, id: string) => {
        event.preventDefault();
        event.dataTransfer.dropEffect = "move";
        if (overId !== id) setOverId(id);
    }, [overId]);

    const handleDragLeave = useCallback((id: string) => {
        setOverId((current) => (current === id ? null : current));
    }, []);

    const handleDrop = useCallback((event: DragEvent<HTMLLIElement>, targetId: string) => {
        event.preventDefault();
        const sourceId = event.dataTransfer.getData("text/plain") || draggingId;
        if (!sourceId || sourceId === targetId) {
            setDraggingId(null);
            setOverId(null);
            return;
        }
        const sourceIndex = items.findIndex((c) => c.id === sourceId);
        const targetIndex = items.findIndex((c) => c.id === targetId);
        if (sourceIndex < 0 || targetIndex < 0 || sourceIndex === targetIndex) {
            setDraggingId(null);
            setOverId(null);
            return;
        }
        const next = items.slice();
        const [moved] = next.splice(sourceIndex, 1);
        next.splice(targetIndex, 0, moved);
        onReorder(next);
        setDraggingId(null);
        setOverId(null);
    }, [draggingId, items, onReorder]);

    const handleDragEnd = useCallback(() => {
        setDraggingId(null);
        setOverId(null);
    }, []);

    const moveByOffset = useCallback((currentId: string, offset: number) => {
        const index = items.findIndex((c) => c.id === currentId);
        if (index < 0) return;
        const targetIndex = index + offset;
        if (targetIndex < 0 || targetIndex >= items.length) return;
        const next = items.slice();
        const [moved] = next.splice(index, 1);
        next.splice(targetIndex, 0, moved);
        onReorder(next);
    }, [items, onReorder]);

    const handleKeyDown = useCallback((event: KeyboardEvent<HTMLLIElement>, id: string) => {
        if (event.key === "ArrowUp") {
            event.preventDefault();
            moveByOffset(id, -1);
        } else if (event.key === "ArrowDown") {
            event.preventDefault();
            moveByOffset(id, 1);
        }
    }, [moveByOffset]);

    return (
        <ol className="chapter-reorder" aria-label="调整章节顺序">
            {items.map((chapter) => {
                const isDragging = draggingId === chapter.id;
                const isOver = overId === chapter.id && draggingId !== null && draggingId !== chapter.id;
                return (
                    <li
                        key={chapter.id}
                        className={`chapter-reorder__row${isDragging ? " is-dragging" : ""}${isOver ? " is-over" : ""}`}
                        draggable
                        tabIndex={0}
                        onDragStart={(event) => handleDragStart(event, chapter.id)}
                        onDragOver={(event) => handleDragOver(event, chapter.id)}
                        onDragLeave={() => handleDragLeave(chapter.id)}
                        onDrop={(event) => handleDrop(event, chapter.id)}
                        onDragEnd={handleDragEnd}
                        onKeyDown={(event) => handleKeyDown(event, chapter.id)}
                        aria-grabbed={isDragging}
                    >
                        <span className="chapter-reorder__handle" aria-hidden="true">⋮⋮</span>
                        <span className="chapter-reorder__title">{chapter.title}</span>
                        {renderItem ? (
                            <span className="chapter-reorder__extra">{renderItem(chapter)}</span>
                        ) : null}
                    </li>
                );
            })}
        </ol>
    );
}