export type MediaStatus = "draft" | "ready" | "failed";
export type MediaScanStatus = "pending" | "clean" | "infected";

export interface MediaAsset {
  id: string;
  ownerUserId: string;
  s3Key: string;
  contentType: string;
  sizeBytes: number;
  status: MediaStatus;
  scanStatus: MediaScanStatus;
  checksumSha256?: string;
  createdAt: string;
  updatedAt: string;
}

export interface DraftLesson {
  clientId: string;
  title: string;
  required: boolean;
  durationSeconds: number;
  mediaAssetId?: string;
  /** 仅作 UI 回显用，不发到后端（后端只收 mediaAssetId）。 */
  mediaUrl?: string;
}

export interface DraftChapter {
  clientId: string;
  title: string;
  lessons: DraftLesson[];
}

export interface CurriculumLessonInput {
  title: string;
  required: boolean;
  durationSeconds: number;
  mediaAssetId?: string;
}

export interface CurriculumChapterInput {
  title: string;
  lessons: CurriculumLessonInput[];
}

let fallbackSequence = 0;

function createClientId(prefix: string): string {
  const randomUUID = globalThis.crypto?.randomUUID?.();
  if (randomUUID) return `${prefix}-${randomUUID}`;

  // 极旧浏览器没有 randomUUID；时间戳加进程内序号仍可保证 React key 稳定。
  fallbackSequence += 1;
  return `${prefix}-${Date.now()}-${fallbackSequence}`;
}

export function createDraftLesson(): DraftLesson {
  return {
    clientId: createClientId("lesson"),
    title: "",
    required: true,
    durationSeconds: 0,
  };
}

export function createDraftChapter(): DraftChapter {
  return {
    clientId: createClientId("chapter"),
    title: "",
    lessons: [createDraftLesson()],
  };
}

export function toCurriculumInput(chapters: DraftChapter[]): CurriculumChapterInput[] {
  return chapters.map((chapter) => ({
    title: chapter.title.trim(),
    lessons: chapter.lessons.map((lesson) => ({
      title: lesson.title.trim(),
      required: lesson.required,
      durationSeconds: lesson.durationSeconds,
      ...(lesson.mediaAssetId ? {mediaAssetId: lesson.mediaAssetId} : {}),
    })),
  }));
}

export function isCurriculumValid(chapters: DraftChapter[]): boolean {
  return chapters.every(
    (chapter) =>
      chapter.title.trim().length > 0 &&
      chapter.lessons.every(
        (lesson) => lesson.title.trim().length > 0 && lesson.durationSeconds >= 0,
      ),
  );
}
