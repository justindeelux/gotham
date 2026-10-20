import{b3 as K,ew as X,b6 as T,f8 as q,b5 as P,f9 as D,bX as _,d as U,b7 as I,ez as Y,ex as J,o as R,c as k,b9 as j,X as $,bG as A,ba as Q,aN as W,c0 as Z,j as y,n as ee,bP as re,M as te,W as oe,f as O,p as se,fO as ne,a_ as ae,gw as ie,bc as le,gx as ce}from"./index-C2Z_mtBh.js";import{t as de}from"./Tag-BO4KtNPE.js";const fe=K&&"loading"in document.createElement("img");function ue(e={}){const{root:i=null}=e;return{hash:`${e.rootMargin||"0px 0px 0px 0px"}-${Array.isArray(e.threshold)?e.threshold.join(","):e.threshold??"0"}`,options:{...e,root:(typeof i=="string"?document.querySelector(i):i)||document.documentElement}}}const F=new WeakMap,M=new WeakMap,C=new WeakMap,he=(e,i,m)=>{if(!e)return()=>{};const s=ue(i),{root:c}=s.options;let n;const f=F.get(c);f?n=f:(n=new Map,F.set(c,n));let a,t;n.has(s.hash)?(t=n.get(s.hash),t[1].has(e)||(a=t[0],t[1].add(e),a.observe(e))):(a=new IntersectionObserver(v=>{v.forEach(g=>{if(g.isIntersecting){const z=M.get(g.target),p=C.get(g.target);z&&z(),p&&(p.value=!0)}})},s.options),a.observe(e),t=[a,new Set([e])],n.set(s.hash,t));let u=!1;const h=()=>{u||(M.delete(e),C.delete(e),u=!0,t[1].has(e)&&(t[0].unobserve(e),t[1].delete(e)),t[1].size<=0&&n.delete(s.hash),n.size||F.delete(c))};return M.set(e,h),C.set(e,m),h},ve=X("n-avatar-group");var be=T("avatar",`
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
`,[q(P("&","--n-merged-color: var(--n-color-modal);")),D(P("&","--n-merged-color: var(--n-color-popover);")),P("img",`
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
 `),_("text","line-height: 1.25")]);const me=["src"],ge={...I.props,size:[String,Number],src:String,circle:{type:Boolean,default:void 0},objectFit:String,round:{type:Boolean,default:void 0},bordered:{type:Boolean,default:void 0},onError:Function,fallbackSrc:String,intersectionObserverOptions:Object,lazy:Boolean,onLoad:Function,renderPlaceholder:Function,renderFallback:Function,imgProps:Object,color:String};var ye=U({name:"Avatar",props:ge,slots:Object,setup(e){const{mergedClsPrefixRef:i,inlineThemeDisabled:m}=Q(e),s=y(!1);let c=null;const n=y(null),f=y(null),a=()=>{const{value:r}=n;if(r&&(c===null||c!==r.innerHTML)){c=r.innerHTML;const{value:o}=f;if(o){const{offsetWidth:d,offsetHeight:b}=o,{offsetWidth:l,offsetHeight:E}=r,x=.9,L=Math.min(d/l*x,b/E*x,1);r.style.transform=`translateX(-50%) translateY(-50%) scale(${L})`}}},t=W(ve,null),u=O(()=>{const{size:r}=e;if(r)return r;const{size:o}=t||{};return o||"medium"}),h=I("Avatar","-avatar",be,ie,e,i),v=W(de,null),g=O(()=>{if(t)return!0;const{round:r,circle:o}=e;return r!==void 0||o!==void 0?r||o:v?v.roundRef.value:!1}),z=O(()=>t?!0:e.bordered||!1),p=O(()=>{const r=u.value,o=g.value,d=z.value,{color:b}=e,{self:{borderRadius:l,fontSize:E,color:x,border:L,colorModal:N,colorPopover:V},common:{cubicBezierEaseInOut:G}}=h.value;let w;return typeof r=="number"?w=`${r}px`:w=h.value.self[le("height",r)],{"--n-font-size":E,"--n-border":d?L:"none","--n-border-radius":o?"50%":l,"--n-color":b||x,"--n-color-modal":b||N,"--n-color-popover":b||V,"--n-bezier":G,"--n-merged-size":`var(--n-avatar-size-override, ${w})`}}),B=m?Z("avatar",O(()=>{const r=u.value,o=g.value,d=z.value,{color:b}=e;let l="";return r&&(typeof r=="number"?l+=`a${r}`:l+=r[0]),o&&(l+="b"),d&&(l+="c"),b&&(l+=ce(b)),l}),p,e):void 0,S=y(!e.lazy);ee(()=>{if(e.lazy&&e.intersectionObserverOptions){let r;const o=re(()=>{r?.(),r=void 0,e.lazy&&(r=he(f.value,e.intersectionObserverOptions,S))});te(()=>{o(),r?.()})}}),oe(()=>e.src||e.imgProps?.src,()=>{s.value=!1});const H=y(!e.lazy);return{textRef:n,selfRef:f,mergedRoundRef:g,mergedClsPrefix:i,fitTextTransform:a,cssVars:m?void 0:p,themeClass:B?.themeClass,onRender:B?.onRender,hasLoadError:s,shouldStartLoading:S,loaded:H,mergedOnError:r=>{if(!S.value)return;s.value=!0;const{onError:o,imgProps:{onError:d}={}}=e;o?.(r),d?.(r)},mergedOnLoad:r=>{const{onLoad:o,imgProps:{onLoad:d}={}}=e;o?.(r),d?.(r),H.value=!0}}},render(){const{$slots:e,src:i,mergedClsPrefix:m,lazy:s,onRender:c,loaded:n,hasLoadError:f,imgProps:a={}}=this;c?.();let t;const u=!n&&!f&&(this.renderPlaceholder?this.renderPlaceholder():this.$slots.placeholder?.());return this.hasLoadError?t=this.renderFallback?this.renderFallback():Y(e.fallback,()=>[(R(),k("img",{src:this.fallbackSrc,style:$({objectFit:this.objectFit})},null,12,me))]):t=J(e.default,h=>{if(h)return R(),se(ne,{key:1,onResize:this.fitTextTransform},{default:()=>(R(),k("span",{ref:"textRef",class:A(`${m}-avatar__text`)},[j(()=>h)],2))},1032,["onResize"]);if(i||a.src){const v=this.src||a.src;return ae("img",{...a,loading:fe&&!this.intersectionObserverOptions&&s?"lazy":"eager",src:s&&this.intersectionObserverOptions?this.shouldStartLoading?v:void 0:v,"data-image-src":v,onLoad:this.mergedOnLoad,onError:this.mergedOnError,style:[a.style||"",{objectFit:this.objectFit},u?{height:"0",width:"0",visibility:"hidden",position:"absolute"}:""]})}}),R(),k("span",{ref:"selfRef",class:A([`${m}-avatar`,this.themeClass]),style:$(this.cssVars)},[j(()=>t),j(()=>s&&u)],6)}});export{ye as A};
