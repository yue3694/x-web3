import {Navigate, useParams} from "react-router-dom";

import {CourseDetail} from "@/features/catalog/CourseDetail";

export default function CoursePage() {
    const {courseId} = useParams();
    if (!courseId) return <Navigate to="/courses" replace />;
    return <CourseDetail courseId={courseId} />;
}
