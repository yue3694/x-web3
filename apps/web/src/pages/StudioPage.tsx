import {RequirePermission} from "@/auth/RequirePermission";
import {CourseEditor} from "@/features/teacher/CourseEditor";

export default function StudioPage() {
    return (
        <RequirePermission code="COURSE_CREATE" fallback={<div className="permission-gate panel" role="alert"><span className="eyebrow">Teacher workspace</span><h1>Teacher access required.</h1><p>Your account needs the COURSE_CREATE permission before you can create or submit courses.</p></div>}>
            <div className="page-stack"><CourseEditor /></div>
        </RequirePermission>
    );
}
