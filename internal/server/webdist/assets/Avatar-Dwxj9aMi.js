import{b7 as G,d7 as q,ba as T,fK as D,b9 as P,fL as J,b_ as _,d as U,bb as W,da as X,d8 as Y,o as R,c as j,bc as w,_ as A,bJ as $,bd as Z,aT as I,c3 as Q,j as y,n as ee,bS as re,M as te,Z as oe,f as O,p as se,go as ne,aA as ae,h6 as ie,bf as le,h7 as ce}from"./index-C7q2uRgZ.js";import{t as de}from"./Tag-CM24GO5q.js";const fe=G&&"loading"in document.createElement("img");function ue(e={}){const{root:i=null}=e;return{hash:`${e.rootMargin||"0px 0px 0px 0px"}-${Array.isArray(e.threshold)?e.threshold.join(","):e.threshold??"0"}`,options:{...e,root:(typeof i=="string"?document.querySelector(i):i)||document.documentElement}}}const F=new WeakMap,M=new WeakMap,C=new WeakMap,he=(e,i,m)=>{if(!e)return()=>{};const s=ue(i),{root:c}=s.options;let n;const f=F.get(c);f?n=f:(n=new Map,F.set(c,n));let a,t;n.has(s.hash)?(t=n.get(s.hash),t[1].has(e)||(a=t[0],t[1].add(e),a.observe(e))):(a=new IntersectionObserver(v=>{v.forEach(g=>{if(g.isIntersecting){const p=M.get(g.target),z=C.get(g.target);p&&p(),z&&(z.value=!0)}})},s.options),a.observe(e),t=[a,new Set([e])],n.set(s.hash,t));let u=!1;const h=()=>{u||(M.delete(e),C.delete(e),u=!0,t[1].has(e)&&(t[0].unobserve(e),t[1].delete(e)),t[1].size<=0&&n.delete(s.hash),n.size||F.delete(c))};return M.set(e,h),C.set(e,m),h},ve=q("n-avatar-group");var be=T("avatar",`
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
`,[D(P("&","--n-merged-color: var(--n-color-modal);")),J(P("&","--n-merged-color: var(--n-color-popover);")),P("img",`
 width: 100%;
 height: 100%;
 `),_("text",`
 white-space: nowrap;
 display: inline-block;
 position: absolute;
 left: 50%;
 top: 50%;
 `),T("icon",`
 vertical-align: bottom;
 font-size: calc(var(--n-merged-size) - 6px);
 `),_("text","line-height: 1.25")]);const me=["src"],ge={...W.props,size:[String,Number],src:String,circle:{type:Boolean,default:void 0},objectFit:String,round:{type:Boolean,default:void 0},bordered:{type:Boolean,default:void 0},onError:Function,fallbackSrc:String,intersectionObserverOptions:Object,lazy:Boolean,onLoad:Function,renderPlaceholder:Function,renderFallback:Function,imgProps:Object,color:String};var ye=U({name:"Avatar",props:ge,slots:Object,setup(e){const{mergedClsPrefixRef:i,inlineThemeDisabled:m}=Z(e),s=y(!1);let c=null;const n=y(null),f=y(null),a=()=>{const{value:r}=n;if(r&&(c===null||c!==r.innerHTML)){c=r.innerHTML;const{value:o}=f;if(o){const{offsetWidth:d,offsetHeight:b}=o,{offsetWidth:l,offsetHeight:E}=r,x=.9,L=Math.min(d/l*x,b/E*x,1);r.style.transform=`translateX(-50%) translateY(-50%) scale(${L})`}}},t=I(ve,null),u=O(()=>{const{size:r}=e;if(r)return r;const{size:o}=t||{};return o||"medium"}),h=W("Avatar","-avatar",be,ie,e,i),v=I(de,null),g=O(()=>{if(t)return!0;const{round:r,circle:o}=e;return r!==void 0||o!==void 0?r||o:v?v.roundRef.value:!1}),p=O(()=>t?!0:e.bordered||!1),z=O(()=>{const r=u.value,o=g.value,d=p.value,{color:b}=e,{self:{borderRadius:l,fontSize:E,color:x,border:L,colorModal:K,colorPopover:N},common:{cubicBezierEaseInOut:V}}=h.value;let k;return typeof r=="number"?k=`${r}px`:k=h.value.self[le("height",r)],{"--n-font-size":E,"--n-border":d?L:"none","--n-border-radius":o?"50%":l,"--n-color":b||x,"--n-color-modal":b||K,"--n-color-popover":b||N,"--n-bezier":V,"--n-merged-size":`var(--n-avatar-size-override, ${k})`}}),B=m?Q("avatar",O(()=>{const r=u.value,o=g.value,d=p.value,{color:b}=e;let l="";return r&&(typeof r=="number"?l+=`a${r}`:l+=r[0]),o&&(l+="b"),d&&(l+="c"),b&&(l+=ce(b)),l}),z,e):void 0,S=y(!e.lazy);ee(()=>{if(e.lazy&&e.intersectionObserverOptions){let r;const o=re(()=>{r?.(),r=void 0,e.lazy&&(r=he(f.value,e.intersectionObserverOptions,S))});te(()=>{o(),r?.()})}}),oe(()=>e.src||e.imgProps?.src,()=>{s.value=!1});const H=y(!e.lazy);return{textRef:n,selfRef:f,mergedRoundRef:g,mergedClsPrefix:i,fitTextTransform:a,cssVars:m?void 0:z,themeClass:B?.themeClass,onRender:B?.onRender,hasLoadError:s,shouldStartLoading:S,loaded:H,mergedOnError:r=>{if(!S.value)return;s.value=!0;const{onError:o,imgProps:{onError:d}={}}=e;o?.(r),d?.(r)},mergedOnLoad:r=>{const{onLoad:o,imgProps:{onLoad:d}={}}=e;o?.(r),d?.(r),H.value=!0}}},render(){const{$slots:e,src:i,mergedClsPrefix:m,lazy:s,onRender:c,loaded:n,hasLoadError:f,imgProps:a={}}=this;c?.();let t;const u=!n&&!f&&(this.renderPlaceholder?this.renderPlaceholder():this.$slots.placeholder?.());return this.hasLoadError?t=this.renderFallback?this.renderFallback():X(e.fallback,()=>[(R(),j("img",{src:this.fallbackSrc,style:A({objectFit:this.objectFit})},null,12,me))]):t=Y(e.default,h=>{if(h)return R(),se(ne,{key:1,onResize:this.fitTextTransform},{default:()=>(R(),j("span",{ref:"textRef",class:$(`${m}-avatar__text`)},[w(()=>h)],2))},1032,["onResize"]);if(i||a.src){const v=this.src||a.src;return ae("img",{...a,loading:fe&&!this.intersectionObserverOptions&&s?"lazy":"eager",src:s&&this.intersectionObserverOptions?this.shouldStartLoading?v:void 0:v,"data-image-src":v,onLoad:this.mergedOnLoad,onError:this.mergedOnError,style:[a.style||"",{objectFit:this.objectFit},u?{height:"0",width:"0",visibility:"hidden",position:"absolute"}:""]})}}),R(),j("span",{ref:"selfRef",class:$([`${m}-avatar`,this.themeClass]),style:A(this.cssVars)},[w(()=>t),w(()=>s&&u)],6)}});export{ye as A};
