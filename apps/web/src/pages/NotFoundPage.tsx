import {Link} from "react-router-dom";

export function NotFoundPage() {
    return <div className="not-found panel"><span className="eyebrow">404 · 路由未找到</span><h1>这个页面不在链上。</h1><p>该地址没有对应的产品路由。</p><Link className="btn btn--primary" to="/">返回首页</Link></div>;
}
