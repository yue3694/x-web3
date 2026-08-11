import {CourseCatalog} from "@/features/catalog/CourseCatalog";
import {TARGET_CHAIN_NAME} from "@/chains";

export default function CatalogPage() {
    return (
        <div className="page-stack">
            <div className="section-heading">
                <div>
                    <span className="eyebrow">课程市场</span>
                    <h1>找到你的下一项链上技能。</h1>
                    <p>搜索已发布的课程、查看完整大纲，然后通过经 {TARGET_CHAIN_NAME} 验证的交易完成报名。</p>
                </div>
            </div>
            <CourseCatalog />
        </div>
    );
}
