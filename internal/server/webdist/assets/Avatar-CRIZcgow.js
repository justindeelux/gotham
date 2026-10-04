import{M as G,b7 as q,P as T,b8 as U,O as k,b9 as X,az as $,d as D,Q as W,ba as Q,ai as Y,o as R,m as J,bb as Z,S as ee,c as w,U as F,a3 as A,a4 as _,V as re,ao as I,ad as te,y,q as O,g as oe,ar as se,K as ae,E as ne,bc as ie,X as le,bd as ce}from"./index-DspG0Jki.js";import{t as de}from"./Tag-BRM1K4jz.js";const ue=G&&"loading"in document.createElement("img");function fe(e={}){const{root:i=null}=e;return{hash:`${e.rootMargin||"0px 0px 0px 0px"}-${Array.isArray(e.threshold)?e.threshold.join(","):e.threshold??"0"}`,options:{...e,root:(typeof i=="string"?document.querySelector(i):i)||document.documentElement}}}const j=new WeakMap,M=new WeakMap,C=new WeakMap,he=(e,i,b)=>{if(!e)return()=>{};const s=fe(i),{root:c}=s.options;let a;const u=j.get(c);u?a=u:(a=new Map,j.set(c,a));let n,t;a.has(s.hash)?(t=a.get(s.hash),t[1].has(e)||(n=t[0],t[1].add(e),n.observe(e))):(n=new IntersectionObserver(v=>{v.forEach(g=>{if(g.isIntersecting){const z=M.get(g.target),p=C.get(g.target);z&&z(),p&&(p.value=!0)}})},s.options),n.observe(e),t=[n,new Set([e])],a.set(s.hash,t));let f=!1;const h=()=>{f||(M.delete(e),C.delete(e),f=!0,t[1].has(e)&&(t[0].unobserve(e),t[1].delete(e)),t[1].size<=0&&a.delete(s.hash),a.size||j.delete(c))};return M.set(e,h),C.set(e,b),h},ve=q("n-avatar-group");var me=T("avatar",`
 width: var(--n-merged-size);
 height: var(--n-merged-size);
 color: #FFF;
 font-size: var(--n-font-size);
 display: inline-flex;
 position: relative;
 overflow: hidden;
 text-align: center;
 border: var(--n-border);
 border-radius: var(--n-border-radius);
 --n-merged-color: var(--n-color);
 background-color: var(--n-merged-color);
 transition:
 border-color .3s var(--n-bezier),
 background-color .3s var(--n-bezier),
 color .3s var(--n-bezier);
`,[U(k("&","--n-merged-color: var(--n-color-modal);")),X(k("&","--n-merged-color: var(--n-color-popover);")),k("img",`
 width: 100%;
 height: 100%;
 `),$("text",`
 white-space: nowrap;
 display: inline-block;
 position: absolute;
 left: 50%;
 top: 50%;
 `),T("icon",`
 vertical-align: bottom;
 font-size: calc(var(--n-merged-size) - 6px);
 `),$("text","line-height: 1.25")]);const be=["src"],ge={...W.props,size:[String,Number],src:String,circle:{type:Boolean,default:void 0},objectFit:String,round:{type:Boolean,default:void 0},bordered:{type:Boolean,default:void 0},onError:Function,fallbackSrc:String,intersectionObserverOptions:Object,lazy:Boolean,onLoad:Function,renderPlaceholder:Function,renderFallback:Function,imgProps:Object,color:String};var ye=D({name:"Avatar",props:ge,slots:Object,setup(e){const{mergedClsPrefixRef:i,inlineThemeDisabled:b}=re(e),s=O(!1);let c=null;const a=O(null),u=O(null),n=()=>{const{value:r}=a;if(r&&(c===null||c!==r.innerHTML)){c=r.innerHTML;const{value:o}=u;if(o){const{offsetWidth:d,offsetHeight:m}=o,{offsetWidth:l,offsetHeight:E}=r,x=.9,L=Math.min(d/l*x,m/E*x,1);r.style.transform=`translateX(-50%) translateY(-50%) scale(${L})`}}},t=I(ve,null),f=y(()=>{const{size:r}=e;if(r)return r;const{size:o}=t||{};return o||"medium"}),h=W("Avatar","-avatar",me,ie,e,i),v=I(de,null),g=y(()=>{if(t)return!0;const{round:r,circle:o}=e;return r!==void 0||o!==void 0?r||o:v?v.roundRef.value:!1}),z=y(()=>t?!0:e.bordered||!1),p=y(()=>{const r=f.value,o=g.value,d=z.value,{color:m}=e,{self:{borderRadius:l,fontSize:E,color:x,border:L,colorModal:V,colorPopover:K},common:{cubicBezierEaseInOut:N}}=h.value;let P;return typeof r=="number"?P=`${r}px`:P=h.value.self[le("height",r)],{"--n-font-size":E,"--n-border":d?L:"none","--n-border-radius":o?"50%":l,"--n-color":m||x,"--n-color-modal":m||V,"--n-color-popover":m||K,"--n-bezier":N,"--n-merged-size":`var(--n-avatar-size-override, ${P})`}}),B=b?te("avatar",y(()=>{const r=f.value,o=g.value,d=z.value,{color:m}=e;let l="";return r&&(typeof r=="number"?l+=`a${r}`:l+=r[0]),o&&(l+="b"),d&&(l+="c"),m&&(l+=ce(m)),l}),p,e):void 0,S=O(!e.lazy);oe(()=>{if(e.lazy&&e.intersectionObserverOptions){let r;const o=se(()=>{r?.(),r=void 0,e.lazy&&(r=he(u.value,e.intersectionObserverOptions,S))});ae(()=>{o(),r?.()})}}),ne(()=>e.src||e.imgProps?.src,()=>{s.value=!1});const H=O(!e.lazy);return{textRef:a,selfRef:u,mergedRoundRef:g,mergedClsPrefix:i,fitTextTransform:n,cssVars:b?void 0:p,themeClass:B?.themeClass,onRender:B?.onRender,hasLoadError:s,shouldStartLoading:S,loaded:H,mergedOnError:r=>{if(!S.value)return;s.value=!0;const{onError:o,imgProps:{onError:d}={}}=e;o?.(r),d?.(r)},mergedOnLoad:r=>{const{onLoad:o,imgProps:{onLoad:d}={}}=e;o?.(r),d?.(r),H.value=!0}}},render(){const{$slots:e,src:i,mergedClsPrefix:b,lazy:s,onRender:c,loaded:a,hasLoadError:u,imgProps:n={}}=this;c?.();let t;const f=!a&&!u&&(this.renderPlaceholder?this.renderPlaceholder():this.$slots.placeholder?.());return this.hasLoadError?t=this.renderFallback?this.renderFallback():Q(e.fallback,()=>[(R(),w("img",{src:this.fallbackSrc,style:A({objectFit:this.objectFit})},null,12,be))]):t=Y(e.default,h=>{if(h)return R(),J(Z,{key:1,onResize:this.fitTextTransform},{default:()=>(R(),w("span",{ref:"textRef",class:_(`${b}-avatar__text`)},[F(()=>h)],2))},1032,["onResize"]);if(i||n.src){const v=this.src||n.src;return ee("img",{...n,loading:ue&&!this.intersectionObserverOptions&&s?"lazy":"eager",src:s&&this.intersectionObserverOptions?this.shouldStartLoading?v:void 0:v,"data-image-src":v,onLoad:this.mergedOnLoad,onError:this.mergedOnError,style:[n.style||"",{objectFit:this.objectFit},f?{height:"0",width:"0",visibility:"hidden",position:"absolute"}:""]})}}),R(),w("span",{ref:"selfRef",class:_([`${b}-avatar`,this.themeClass]),style:A(this.cssVars)},[F(()=>t),F(()=>s&&f)],6)}});export{ye as A};
