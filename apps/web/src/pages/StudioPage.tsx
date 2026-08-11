import {RequirePermission} from "@/auth/RequirePermission";
import {CourseEditor} from "@/features/teacher/CourseEditor";

export default function StudioPage() {
    return (
        <RequirePermission code="COURSE_CREATE" fallback={<div className="permission-gate panel" role="alert"><span className="eyebrow">讲师工作区</span><h1>需要讲师权限。</h1><p>你的账户需要先获得 COURSE_CREATE 权限，才能创建或提交课程。</p></div>}>
            <div className="page-stack"><CourseEditor /></div>
        </RequirePermission>
    );
}
