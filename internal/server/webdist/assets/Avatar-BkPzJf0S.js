import{aS as Y,dY as G,aV as A,eA as Z,aU as k,eB as U,bK as H,d as q,aW as I,d$ as D,dZ as X,o as R,c as j,aY as w,ab as T,bt as W,aZ as J,aA as _,bP as Q,j as y,n as ee,bC as re,aa as te,P as oe,f as O,p as se,fe as ae,aP as ne,fY as ie,a$ as le,fZ as ce}from"./index-DVgPipsm.js";import{t as de}from"./Tag-DsOAIjly.js";const fe=Y&&"loading"in document.createElement("img");function ue(e={}){const{root:i=null}=e;return{hash:`${e.rootMargin||"0px 0px 0px 0px"}-${Array.isArray(e.threshold)?e.threshold.join(","):e.threshold??"0"}`,options:{...e,root:(typeof i=="string"?document.querySelector(i):i)||document.documentElement}}}const F=new WeakMap,C=new WeakMap,B=new WeakMap,he=(e,i,g)=>{if(!e)return()=>{};const s=ue(i),{root:c}=s.options;let a;const f=F.get(c);f?a=f:(a=new Map,F.set(c,a));let n,t;a.has(s.hash)?(t=a.get(s.hash),t[1].has(e)||(n=t[0],t[1].add(e),n.observe(e))):(n=new IntersectionObserver(v=>{v.forEach(b=>{if(b.isIntersecting){const p=C.get(b.target),z=B.get(b.target);p&&p(),z&&(z.value=!0)}})},s.options),n.observe(e),t=[n,new Set([e])],a.set(s.hash,t));let u=!1;const h=()=>{u||(C.delete(e),B.delete(e),u=!0,t[1].has(e)&&(t[0].unobserve(e),t[1].delete(e)),t[1].size<=0&&a.delete(s.hash),a.size||F.delete(c))};return C.set(e,h),B.set(e,g),h},ve=G("n-avatar-group");var me=A("avatar",`
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
`,[Z(k("&","--n-merged-color: var(--n-color-modal);")),U(k("&","--n-merged-color: var(--n-color-popover);")),k("img",`
 width: 100%;
 height: 100%;
 `),H("text",`
 white-space: nowrap;
 display: inline-block;
 position: absolute;
 left: 50%;
 top: 50%;
 `),A("icon",`
 vertical-align: bottom;
 font-size: calc(var(--n-merged-size) - 6px);
 `),H("text","line-height: 1.25")]);const ge=["src"],be={...I.props,size:[String,Number],src:String,circle:{type:Boolean,default:void 0},objectFit:String,round:{type:Boolean,default:void 0},bordered:{type:Boolean,default:void 0},onError:Function,fallbackSrc:String,intersectionObserverOptions:Object,lazy:Boolean,onLoad:Function,renderPlaceholder:Function,renderFallback:Function,imgProps:Object,color:String};var ye=q({name:"Avatar",props:be,slots:Object,setup(e){const{mergedClsPrefixRef:i,inlineThemeDisabled:g}=J(e),s=y(!1);let c=null;const a=y(null),f=y(null),n=()=>{const{value:r}=a;if(r&&(c===null||c!==r.innerHTML)){c=r.innerHTML;const{value:o}=f;if(o){const{offsetWidth:d,offsetHeight:m}=o,{offsetWidth:l,offsetHeight:E}=r,x=.9,L=Math.min(d/l*x,m/E*x,1);r.style.transform=`translateX(-50%) translateY(-50%) scale(${L})`}}},t=_(ve,null),u=O(()=>{const{size:r}=e;if(r)return r;const{size:o}=t||{};return o||"medium"}),h=I("Avatar","-avatar",me,ie,e,i),v=_(de,null),b=O(()=>{if(t)return!0;const{round:r,circle:o}=e;return r!==void 0||o!==void 0?r||o:v?v.roundRef.value:!1}),p=O(()=>t?!0:e.bordered||!1),z=O(()=>{const r=u.value,o=b.value,d=p.value,{color:m}=e,{self:{borderRadius:l,fontSize:E,color:x,border:L,colorModal:V,colorPopover:K},common:{cubicBezierEaseInOut:N}}=h.value;let P;return typeof r=="number"?P=`${r}px`:P=h.value.self[le("height",r)],{"--n-font-size":E,"--n-border":d?L:"none","--n-border-radius":o?"50%":l,"--n-color":m||x,"--n-color-modal":m||V,"--n-color-popover":m||K,"--n-bezier":N,"--n-merged-size":`var(--n-avatar-size-override, ${P})`}}),M=g?Q("avatar",O(()=>{const r=u.value,o=b.value,d=p.value,{color:m}=e;let l="";return r&&(typeof r=="number"?l+=`a${r}`:l+=r[0]),o&&(l+="b"),d&&(l+="c"),m&&(l+=ce(m)),l}),z,e):void 0,S=y(!e.lazy);ee(()=>{if(e.lazy&&e.intersectionObserverOptions){let r;const o=re(()=>{r?.(),r=void 0,e.lazy&&(r=he(f.value,e.intersectionObserverOptions,S))});te(()=>{o(),r?.()})}}),oe(()=>e.src||e.imgProps?.src,()=>{s.value=!1});const $=y(!e.lazy);return{textRef:a,selfRef:f,mergedRoundRef:b,mergedClsPrefix:i,fitTextTransform:n,cssVars:g?void 0:z,themeClass:M?.themeClass,onRender:M?.onRender,hasLoadError:s,shouldStartLoading:S,loaded:$,mergedOnError:r=>{if(!S.value)return;s.value=!0;const{onError:o,imgProps:{onError:d}={}}=e;o?.(r),d?.(r)},mergedOnLoad:r=>{const{onLoad:o,imgProps:{onLoad:d}={}}=e;o?.(r),d?.(r),$.value=!0}}},render(){const{$slots:e,src:i,mergedClsPrefix:g,lazy:s,onRender:c,loaded:a,hasLoadError:f,imgProps:n={}}=this;c?.();let t;const u=!a&&!f&&(this.renderPlaceholder?this.renderPlaceholder():this.$slots.placeholder?.());return this.hasLoadError?t=this.renderFallback?this.renderFallback():D(e.fallback,()=>[(R(),j("img",{src:this.fallbackSrc,style:T({objectFit:this.objectFit})},null,12,ge))]):t=X(e.default,h=>{if(h)return R(),se(ae,{key:1,onResize:this.fitTextTransform},{default:()=>(R(),j("span",{ref:"textRef",class:W(`${g}-avatar__text`)},[w(()=>h)],2))},1032,["onResize"]);if(i||n.src){const v=this.src||n.src;return ne("img",{...n,loading:fe&&!this.intersectionObserverOptions&&s?"lazy":"eager",src:s&&this.intersectionObserverOptions?this.shouldStartLoading?v:void 0:v,"data-image-src":v,onLoad:this.mergedOnLoad,onError:this.mergedOnError,style:[n.style||"",{objectFit:this.objectFit},u?{height:"0",width:"0",visibility:"hidden",position:"absolute"}:""]})}}),R(),j("span",{ref:"selfRef",class:W([`${g}-avatar`,this.themeClass]),style:T(this.cssVars)},[w(()=>t),w(()=>s&&u)],6)}});export{ye as A};
