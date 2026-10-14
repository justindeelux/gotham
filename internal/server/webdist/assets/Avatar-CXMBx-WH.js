import{a0 as K,c$ as q,a3 as T,dF as X,a2 as F,dG as Y,aO as $,d as D,a4 as I,d2 as U,d0 as J,o as R,c as P,a6 as j,aq as A,av as W,a7 as Q,Y as _,aT as Z,p as y,k as ee,aG as re,X as te,L as oe,g as O,x as se,ej as ae,W as ne,f0 as ie,a9 as le,f1 as ce}from"./index-4WJT4k6z.js";import{t as de}from"./Tag-DNrx7xRD.js";const ue=K&&"loading"in document.createElement("img");function fe(e={}){const{root:i=null}=e;return{hash:`${e.rootMargin||"0px 0px 0px 0px"}-${Array.isArray(e.threshold)?e.threshold.join(","):e.threshold??"0"}`,options:{...e,root:(typeof i=="string"?document.querySelector(i):i)||document.documentElement}}}const w=new WeakMap,C=new WeakMap,M=new WeakMap,he=(e,i,g)=>{if(!e)return()=>{};const s=fe(i),{root:c}=s.options;let a;const u=w.get(c);u?a=u:(a=new Map,w.set(c,a));let n,t;a.has(s.hash)?(t=a.get(s.hash),t[1].has(e)||(n=t[0],t[1].add(e),n.observe(e))):(n=new IntersectionObserver(v=>{v.forEach(b=>{if(b.isIntersecting){const p=C.get(b.target),z=M.get(b.target);p&&p(),z&&(z.value=!0)}})},s.options),n.observe(e),t=[n,new Set([e])],a.set(s.hash,t));let f=!1;const h=()=>{f||(C.delete(e),M.delete(e),f=!0,t[1].has(e)&&(t[0].unobserve(e),t[1].delete(e)),t[1].size<=0&&a.delete(s.hash),a.size||w.delete(c))};return C.set(e,h),M.set(e,g),h},ve=q("n-avatar-group");var me=T("avatar",`
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
`,[X(F("&","--n-merged-color: var(--n-color-modal);")),Y(F("&","--n-merged-color: var(--n-color-popover);")),F("img",`
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
 `),$("text","line-height: 1.25")]);const ge=["src"],be={...I.props,size:[String,Number],src:String,circle:{type:Boolean,default:void 0},objectFit:String,round:{type:Boolean,default:void 0},bordered:{type:Boolean,default:void 0},onError:Function,fallbackSrc:String,intersectionObserverOptions:Object,lazy:Boolean,onLoad:Function,renderPlaceholder:Function,renderFallback:Function,imgProps:Object,color:String};var ye=D({name:"Avatar",props:be,slots:Object,setup(e){const{mergedClsPrefixRef:i,inlineThemeDisabled:g}=Q(e),s=y(!1);let c=null;const a=y(null),u=y(null),n=()=>{const{value:r}=a;if(r&&(c===null||c!==r.innerHTML)){c=r.innerHTML;const{value:o}=u;if(o){const{offsetWidth:d,offsetHeight:m}=o,{offsetWidth:l,offsetHeight:E}=r,x=.9,L=Math.min(d/l*x,m/E*x,1);r.style.transform=`translateX(-50%) translateY(-50%) scale(${L})`}}},t=_(ve,null),f=O(()=>{const{size:r}=e;if(r)return r;const{size:o}=t||{};return o||"medium"}),h=I("Avatar","-avatar",me,ie,e,i),v=_(de,null),b=O(()=>{if(t)return!0;const{round:r,circle:o}=e;return r!==void 0||o!==void 0?r||o:v?v.roundRef.value:!1}),p=O(()=>t?!0:e.bordered||!1),z=O(()=>{const r=f.value,o=b.value,d=p.value,{color:m}=e,{self:{borderRadius:l,fontSize:E,color:x,border:L,colorModal:G,colorPopover:N},common:{cubicBezierEaseInOut:V}}=h.value;let k;return typeof r=="number"?k=`${r}px`:k=h.value.self[le("height",r)],{"--n-font-size":E,"--n-border":d?L:"none","--n-border-radius":o?"50%":l,"--n-color":m||x,"--n-color-modal":m||G,"--n-color-popover":m||N,"--n-bezier":V,"--n-merged-size":`var(--n-avatar-size-override, ${k})`}}),B=g?Z("avatar",O(()=>{const r=f.value,o=b.value,d=p.value,{color:m}=e;let l="";return r&&(typeof r=="number"?l+=`a${r}`:l+=r[0]),o&&(l+="b"),d&&(l+="c"),m&&(l+=ce(m)),l}),z,e):void 0,S=y(!e.lazy);ee(()=>{if(e.lazy&&e.intersectionObserverOptions){let r;const o=re(()=>{r?.(),r=void 0,e.lazy&&(r=he(u.value,e.intersectionObserverOptions,S))});te(()=>{o(),r?.()})}}),oe(()=>e.src||e.imgProps?.src,()=>{s.value=!1});const H=y(!e.lazy);return{textRef:a,selfRef:u,mergedRoundRef:b,mergedClsPrefix:i,fitTextTransform:n,cssVars:g?void 0:z,themeClass:B?.themeClass,onRender:B?.onRender,hasLoadError:s,shouldStartLoading:S,loaded:H,mergedOnError:r=>{if(!S.value)return;s.value=!0;const{onError:o,imgProps:{onError:d}={}}=e;o?.(r),d?.(r)},mergedOnLoad:r=>{const{onLoad:o,imgProps:{onLoad:d}={}}=e;o?.(r),d?.(r),H.value=!0}}},render(){const{$slots:e,src:i,mergedClsPrefix:g,lazy:s,onRender:c,loaded:a,hasLoadError:u,imgProps:n={}}=this;c?.();let t;const f=!a&&!u&&(this.renderPlaceholder?this.renderPlaceholder():this.$slots.placeholder?.());return this.hasLoadError?t=this.renderFallback?this.renderFallback():U(e.fallback,()=>[(R(),P("img",{src:this.fallbackSrc,style:A({objectFit:this.objectFit})},null,12,ge))]):t=J(e.default,h=>{if(h)return R(),se(ae,{key:1,onResize:this.fitTextTransform},{default:()=>(R(),P("span",{ref:"textRef",class:W(`${g}-avatar__text`)},[j(()=>h)],2))},1032,["onResize"]);if(i||n.src){const v=this.src||n.src;return ne("img",{...n,loading:ue&&!this.intersectionObserverOptions&&s?"lazy":"eager",src:s&&this.intersectionObserverOptions?this.shouldStartLoading?v:void 0:v,"data-image-src":v,onLoad:this.mergedOnLoad,onError:this.mergedOnError,style:[n.style||"",{objectFit:this.objectFit},f?{height:"0",width:"0",visibility:"hidden",position:"absolute"}:""]})}}),R(),P("span",{ref:"selfRef",class:W([`${g}-avatar`,this.themeClass]),style:A(this.cssVars)},[j(()=>t),j(()=>s&&f)],6)}});export{ye as A};
