import{aQ as X,dW as G,aT as T,ey as U,aS as k,ez as Z,bI as W,d as q,aU as _,dZ as D,dX as Q,o as R,c as w,aW as j,ab as A,br as $,aX as Y,aw as I,bN as J,j as y,n as ee,bA as re,aa as te,P as oe,f as O,p as se,fc as ae,aN as ne,fW as ie,aZ as le,fX as ce}from"./index-DS34thFf.js";import{t as de}from"./Tag-4pBu7fvv.js";const fe=X&&"loading"in document.createElement("img");function ue(e={}){const{root:i=null}=e;return{hash:`${e.rootMargin||"0px 0px 0px 0px"}-${Array.isArray(e.threshold)?e.threshold.join(","):e.threshold??"0"}`,options:{...e,root:(typeof i=="string"?document.querySelector(i):i)||document.documentElement}}}const F=new WeakMap,C=new WeakMap,M=new WeakMap,he=(e,i,g)=>{if(!e)return()=>{};const s=ue(i),{root:c}=s.options;let a;const f=F.get(c);f?a=f:(a=new Map,F.set(c,a));let n,t;a.has(s.hash)?(t=a.get(s.hash),t[1].has(e)||(n=t[0],t[1].add(e),n.observe(e))):(n=new IntersectionObserver(v=>{v.forEach(b=>{if(b.isIntersecting){const z=C.get(b.target),p=M.get(b.target);z&&z(),p&&(p.value=!0)}})},s.options),n.observe(e),t=[n,new Set([e])],a.set(s.hash,t));let u=!1;const h=()=>{u||(C.delete(e),M.delete(e),u=!0,t[1].has(e)&&(t[0].unobserve(e),t[1].delete(e)),t[1].size<=0&&a.delete(s.hash),a.size||F.delete(c))};return C.set(e,h),M.set(e,g),h},ve=G("n-avatar-group");var me=T("avatar",`
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
`,[U(k("&","--n-merged-color: var(--n-color-modal);")),Z(k("&","--n-merged-color: var(--n-color-popover);")),k("img",`
 width: 100%;
 height: 100%;
 `),W("text",`
 white-space: nowrap;
 display: inline-block;
 position: absolute;
 left: 50%;
 top: 50%;
 `),T("icon",`
 vertical-align: bottom;
 font-size: calc(var(--n-merged-size) - 6px);
 `),W("text","line-height: 1.25")]);const ge=["src"],be={..._.props,size:[String,Number],src:String,circle:{type:Boolean,default:void 0},objectFit:String,round:{type:Boolean,default:void 0},bordered:{type:Boolean,default:void 0},onError:Function,fallbackSrc:String,intersectionObserverOptions:Object,lazy:Boolean,onLoad:Function,renderPlaceholder:Function,renderFallback:Function,imgProps:Object,color:String};var ye=q({name:"Avatar",props:be,slots:Object,setup(e){const{mergedClsPrefixRef:i,inlineThemeDisabled:g}=Y(e),s=y(!1);let c=null;const a=y(null),f=y(null),n=()=>{const{value:r}=a;if(r&&(c===null||c!==r.innerHTML)){c=r.innerHTML;const{value:o}=f;if(o){const{offsetWidth:d,offsetHeight:m}=o,{offsetWidth:l,offsetHeight:E}=r,x=.9,L=Math.min(d/l*x,m/E*x,1);r.style.transform=`translateX(-50%) translateY(-50%) scale(${L})`}}},t=I(ve,null),u=O(()=>{const{size:r}=e;if(r)return r;const{size:o}=t||{};return o||"medium"}),h=_("Avatar","-avatar",me,ie,e,i),v=I(de,null),b=O(()=>{if(t)return!0;const{round:r,circle:o}=e;return r!==void 0||o!==void 0?r||o:v?v.roundRef.value:!1}),z=O(()=>t?!0:e.bordered||!1),p=O(()=>{const r=u.value,o=b.value,d=z.value,{color:m}=e,{self:{borderRadius:l,fontSize:E,color:x,border:L,colorModal:N,colorPopover:V},common:{cubicBezierEaseInOut:K}}=h.value;let P;return typeof r=="number"?P=`${r}px`:P=h.value.self[le("height",r)],{"--n-font-size":E,"--n-border":d?L:"none","--n-border-radius":o?"50%":l,"--n-color":m||x,"--n-color-modal":m||N,"--n-color-popover":m||V,"--n-bezier":K,"--n-merged-size":`var(--n-avatar-size-override, ${P})`}}),B=g?J("avatar",O(()=>{const r=u.value,o=b.value,d=z.value,{color:m}=e;let l="";return r&&(typeof r=="number"?l+=`a${r}`:l+=r[0]),o&&(l+="b"),d&&(l+="c"),m&&(l+=ce(m)),l}),p,e):void 0,S=y(!e.lazy);ee(()=>{if(e.lazy&&e.intersectionObserverOptions){let r;const o=re(()=>{r?.(),r=void 0,e.lazy&&(r=he(f.value,e.intersectionObserverOptions,S))});te(()=>{o(),r?.()})}}),oe(()=>e.src||e.imgProps?.src,()=>{s.value=!1});const H=y(!e.lazy);return{textRef:a,selfRef:f,mergedRoundRef:b,mergedClsPrefix:i,fitTextTransform:n,cssVars:g?void 0:p,themeClass:B?.themeClass,onRender:B?.onRender,hasLoadError:s,shouldStartLoading:S,loaded:H,mergedOnError:r=>{if(!S.value)return;s.value=!0;const{onError:o,imgProps:{onError:d}={}}=e;o?.(r),d?.(r)},mergedOnLoad:r=>{const{onLoad:o,imgProps:{onLoad:d}={}}=e;o?.(r),d?.(r),H.value=!0}}},render(){const{$slots:e,src:i,mergedClsPrefix:g,lazy:s,onRender:c,loaded:a,hasLoadError:f,imgProps:n={}}=this;c?.();let t;const u=!a&&!f&&(this.renderPlaceholder?this.renderPlaceholder():this.$slots.placeholder?.());return this.hasLoadError?t=this.renderFallback?this.renderFallback():D(e.fallback,()=>[(R(),w("img",{src:this.fallbackSrc,style:A({objectFit:this.objectFit})},null,12,ge))]):t=Q(e.default,h=>{if(h)return R(),se(ae,{key:1,onResize:this.fitTextTransform},{default:()=>(R(),w("span",{ref:"textRef",class:$(`${g}-avatar__text`)},[j(()=>h)],2))},1032,["onResize"]);if(i||n.src){const v=this.src||n.src;return ne("img",{...n,loading:fe&&!this.intersectionObserverOptions&&s?"lazy":"eager",src:s&&this.intersectionObserverOptions?this.shouldStartLoading?v:void 0:v,"data-image-src":v,onLoad:this.mergedOnLoad,onError:this.mergedOnError,style:[n.style||"",{objectFit:this.objectFit},u?{height:"0",width:"0",visibility:"hidden",position:"absolute"}:""]})}}),R(),w("span",{ref:"selfRef",class:$([`${g}-avatar`,this.themeClass]),style:A(this.cssVars)},[j(()=>t),j(()=>s&&u)],6)}});export{ye as A};
