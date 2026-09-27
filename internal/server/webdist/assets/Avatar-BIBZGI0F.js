import{E as G,aX as q,I as T,bc as U,H as k,bd as X,an as $,d as D,J as W,aU as J,T as Y,o as R,l as Z,bn as Q,K as ee,c as w,M as F,Z as A,S as I,N as re,ac as _,a5 as te,y,q as O,g as oe,af as se,ag as ne,z as ae,bK as ie,P as le,bL as ce}from"./index-DKCDjb82.js";import{t as de}from"./servers-C6tq74F0.js";const ue=G&&"loading"in document.createElement("img");function fe(e={}){const{root:i=null}=e;return{hash:`${e.rootMargin||"0px 0px 0px 0px"}-${Array.isArray(e.threshold)?e.threshold.join(","):e.threshold??"0"}`,options:{...e,root:(typeof i=="string"?document.querySelector(i):i)||document.documentElement}}}const j=new WeakMap,M=new WeakMap,C=new WeakMap,he=(e,i,g)=>{if(!e)return()=>{};const s=fe(i),{root:c}=s.options;let n;const u=j.get(c);u?n=u:(n=new Map,j.set(c,n));let a,t;n.has(s.hash)?(t=n.get(s.hash),t[1].has(e)||(a=t[0],t[1].add(e),a.observe(e))):(a=new IntersectionObserver(v=>{v.forEach(b=>{if(b.isIntersecting){const z=M.get(b.target),p=C.get(b.target);z&&z(),p&&(p.value=!0)}})},s.options),a.observe(e),t=[a,new Set([e])],n.set(s.hash,t));let f=!1;const h=()=>{f||(M.delete(e),C.delete(e),f=!0,t[1].has(e)&&(t[0].unobserve(e),t[1].delete(e)),t[1].size<=0&&n.delete(s.hash),n.size||j.delete(c))};return M.set(e,h),C.set(e,g),h},ve=q("n-avatar-group");var me=T("avatar",`
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
 `),$("text","line-height: 1.25")]);const ge=["src"],be={...W.props,size:[String,Number],src:String,circle:{type:Boolean,default:void 0},objectFit:String,round:{type:Boolean,default:void 0},bordered:{type:Boolean,default:void 0},onError:Function,fallbackSrc:String,intersectionObserverOptions:Object,lazy:Boolean,onLoad:Function,renderPlaceholder:Function,renderFallback:Function,imgProps:Object,color:String};var ye=D({name:"Avatar",props:be,slots:Object,setup(e){const{mergedClsPrefixRef:i,inlineThemeDisabled:g}=re(e),s=O(!1);let c=null;const n=O(null),u=O(null),a=()=>{const{value:r}=n;if(r&&(c===null||c!==r.innerHTML)){c=r.innerHTML;const{value:o}=u;if(o){const{offsetWidth:d,offsetHeight:m}=o,{offsetWidth:l,offsetHeight:E}=r,x=.9,L=Math.min(d/l*x,m/E*x,1);r.style.transform=`translateX(-50%) translateY(-50%) scale(${L})`}}},t=_(ve,null),f=y(()=>{const{size:r}=e;if(r)return r;const{size:o}=t||{};return o||"medium"}),h=W("Avatar","-avatar",me,ie,e,i),v=_(de,null),b=y(()=>{if(t)return!0;const{round:r,circle:o}=e;return r!==void 0||o!==void 0?r||o:v?v.roundRef.value:!1}),z=y(()=>t?!0:e.bordered||!1),p=y(()=>{const r=f.value,o=b.value,d=z.value,{color:m}=e,{self:{borderRadius:l,fontSize:E,color:x,border:L,colorModal:K,colorPopover:N},common:{cubicBezierEaseInOut:V}}=h.value;let P;return typeof r=="number"?P=`${r}px`:P=h.value.self[le("height",r)],{"--n-font-size":E,"--n-border":d?L:"none","--n-border-radius":o?"50%":l,"--n-color":m||x,"--n-color-modal":m||K,"--n-color-popover":m||N,"--n-bezier":V,"--n-merged-size":`var(--n-avatar-size-override, ${P})`}}),H=g?te("avatar",y(()=>{const r=f.value,o=b.value,d=z.value,{color:m}=e;let l="";return r&&(typeof r=="number"?l+=`a${r}`:l+=r[0]),o&&(l+="b"),d&&(l+="c"),m&&(l+=ce(m)),l}),p,e):void 0,S=O(!e.lazy);oe(()=>{if(e.lazy&&e.intersectionObserverOptions){let r;const o=se(()=>{r?.(),r=void 0,e.lazy&&(r=he(u.value,e.intersectionObserverOptions,S))});ne(()=>{o(),r?.()})}}),ae(()=>e.src||e.imgProps?.src,()=>{s.value=!1});const B=O(!e.lazy);return{textRef:n,selfRef:u,mergedRoundRef:b,mergedClsPrefix:i,fitTextTransform:a,cssVars:g?void 0:p,themeClass:H?.themeClass,onRender:H?.onRender,hasLoadError:s,shouldStartLoading:S,loaded:B,mergedOnError:r=>{if(!S.value)return;s.value=!0;const{onError:o,imgProps:{onError:d}={}}=e;o?.(r),d?.(r)},mergedOnLoad:r=>{const{onLoad:o,imgProps:{onLoad:d}={}}=e;o?.(r),d?.(r),B.value=!0}}},render(){const{$slots:e,src:i,mergedClsPrefix:g,lazy:s,onRender:c,loaded:n,hasLoadError:u,imgProps:a={}}=this;c?.();let t;const f=!n&&!u&&(this.renderPlaceholder?this.renderPlaceholder():this.$slots.placeholder?.());return this.hasLoadError?t=this.renderFallback?this.renderFallback():J(e.fallback,()=>[(R(),w("img",{src:this.fallbackSrc,style:A({objectFit:this.objectFit})},null,12,ge))]):t=Y(e.default,h=>{if(h)return R(),Z(Q,{key:1,onResize:this.fitTextTransform},{default:()=>(R(),w("span",{ref:"textRef",class:I(`${g}-avatar__text`)},[F(()=>h)],2))},1032,["onResize"]);if(i||a.src){const v=this.src||a.src;return ee("img",{...a,loading:ue&&!this.intersectionObserverOptions&&s?"lazy":"eager",src:s&&this.intersectionObserverOptions?this.shouldStartLoading?v:void 0:v,"data-image-src":v,onLoad:this.mergedOnLoad,onError:this.mergedOnError,style:[a.style||"",{objectFit:this.objectFit},f?{height:"0",width:"0",visibility:"hidden",position:"absolute"}:""]})}}),R(),w("span",{ref:"selfRef",class:I([`${g}-avatar`,this.themeClass]),style:A(this.cssVars)},[F(()=>t),F(()=>s&&f)],6)}});export{ye as A};
