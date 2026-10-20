import{b2 as G,en as q,b5 as H,e$ as D,b4 as k,f0 as U,bW as T,d as X,b6 as I,eq as Y,eo as Z,o as R,c as P,b8 as j,W,bF as A,b9 as J,aM as _,b$ as Q,j as y,n as ee,bO as re,M as te,V as oe,f as O,p as se,fF as ne,aZ as ae,gn as ie,bb as le,go as ce}from"./index-HjuIXXkU.js";import{t as de}from"./Tag-DIXPimss.js";const ue=G&&"loading"in document.createElement("img");function fe(e={}){const{root:i=null}=e;return{hash:`${e.rootMargin||"0px 0px 0px 0px"}-${Array.isArray(e.threshold)?e.threshold.join(","):e.threshold??"0"}`,options:{...e,root:(typeof i=="string"?document.querySelector(i):i)||document.documentElement}}}const w=new WeakMap,M=new WeakMap,C=new WeakMap,he=(e,i,m)=>{if(!e)return()=>{};const s=fe(i),{root:c}=s.options;let n;const u=w.get(c);u?n=u:(n=new Map,w.set(c,n));let a,t;n.has(s.hash)?(t=n.get(s.hash),t[1].has(e)||(a=t[0],t[1].add(e),a.observe(e))):(a=new IntersectionObserver(v=>{v.forEach(g=>{if(g.isIntersecting){const p=M.get(g.target),z=C.get(g.target);p&&p(),z&&(z.value=!0)}})},s.options),a.observe(e),t=[a,new Set([e])],n.set(s.hash,t));let f=!1;const h=()=>{f||(M.delete(e),C.delete(e),f=!0,t[1].has(e)&&(t[0].unobserve(e),t[1].delete(e)),t[1].size<=0&&n.delete(s.hash),n.size||w.delete(c))};return M.set(e,h),C.set(e,m),h},ve=q("n-avatar-group");var be=H("avatar",`
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
`,[D(k("&","--n-merged-color: var(--n-color-modal);")),U(k("&","--n-merged-color: var(--n-color-popover);")),k("img",`
 width: 100%;
 height: 100%;
 `),T("text",`
 white-space: nowrap;
 display: inline-block;
 position: absolute;
 left: 50%;
 top: 50%;
 `),H("icon",`
 vertical-align: bottom;
 font-size: calc(var(--n-merged-size) - 6px);
 `),T("text","line-height: 1.25")]);const me=["src"],ge={...I.props,size:[String,Number],src:String,circle:{type:Boolean,default:void 0},objectFit:String,round:{type:Boolean,default:void 0},bordered:{type:Boolean,default:void 0},onError:Function,fallbackSrc:String,intersectionObserverOptions:Object,lazy:Boolean,onLoad:Function,renderPlaceholder:Function,renderFallback:Function,imgProps:Object,color:String};var ye=X({name:"Avatar",props:ge,slots:Object,setup(e){const{mergedClsPrefixRef:i,inlineThemeDisabled:m}=J(e),s=y(!1);let c=null;const n=y(null),u=y(null),a=()=>{const{value:r}=n;if(r&&(c===null||c!==r.innerHTML)){c=r.innerHTML;const{value:o}=u;if(o){const{offsetWidth:d,offsetHeight:b}=o,{offsetWidth:l,offsetHeight:E}=r,x=.9,L=Math.min(d/l*x,b/E*x,1);r.style.transform=`translateX(-50%) translateY(-50%) scale(${L})`}}},t=_(ve,null),f=O(()=>{const{size:r}=e;if(r)return r;const{size:o}=t||{};return o||"medium"}),h=I("Avatar","-avatar",be,ie,e,i),v=_(de,null),g=O(()=>{if(t)return!0;const{round:r,circle:o}=e;return r!==void 0||o!==void 0?r||o:v?v.roundRef.value:!1}),p=O(()=>t?!0:e.bordered||!1),z=O(()=>{const r=f.value,o=g.value,d=p.value,{color:b}=e,{self:{borderRadius:l,fontSize:E,color:x,border:L,colorModal:V,colorPopover:N},common:{cubicBezierEaseInOut:K}}=h.value;let F;return typeof r=="number"?F=`${r}px`:F=h.value.self[le("height",r)],{"--n-font-size":E,"--n-border":d?L:"none","--n-border-radius":o?"50%":l,"--n-color":b||x,"--n-color-modal":b||V,"--n-color-popover":b||N,"--n-bezier":K,"--n-merged-size":`var(--n-avatar-size-override, ${F})`}}),$=m?Q("avatar",O(()=>{const r=f.value,o=g.value,d=p.value,{color:b}=e;let l="";return r&&(typeof r=="number"?l+=`a${r}`:l+=r[0]),o&&(l+="b"),d&&(l+="c"),b&&(l+=ce(b)),l}),z,e):void 0,S=y(!e.lazy);ee(()=>{if(e.lazy&&e.intersectionObserverOptions){let r;const o=re(()=>{r?.(),r=void 0,e.lazy&&(r=he(u.value,e.intersectionObserverOptions,S))});te(()=>{o(),r?.()})}}),oe(()=>e.src||e.imgProps?.src,()=>{s.value=!1});const B=y(!e.lazy);return{textRef:n,selfRef:u,mergedRoundRef:g,mergedClsPrefix:i,fitTextTransform:a,cssVars:m?void 0:z,themeClass:$?.themeClass,onRender:$?.onRender,hasLoadError:s,shouldStartLoading:S,loaded:B,mergedOnError:r=>{if(!S.value)return;s.value=!0;const{onError:o,imgProps:{onError:d}={}}=e;o?.(r),d?.(r)},mergedOnLoad:r=>{const{onLoad:o,imgProps:{onLoad:d}={}}=e;o?.(r),d?.(r),B.value=!0}}},render(){const{$slots:e,src:i,mergedClsPrefix:m,lazy:s,onRender:c,loaded:n,hasLoadError:u,imgProps:a={}}=this;c?.();let t;const f=!n&&!u&&(this.renderPlaceholder?this.renderPlaceholder():this.$slots.placeholder?.());return this.hasLoadError?t=this.renderFallback?this.renderFallback():Y(e.fallback,()=>[(R(),P("img",{src:this.fallbackSrc,style:W({objectFit:this.objectFit})},null,12,me))]):t=Z(e.default,h=>{if(h)return R(),se(ne,{key:1,onResize:this.fitTextTransform},{default:()=>(R(),P("span",{ref:"textRef",class:A(`${m}-avatar__text`)},[j(()=>h)],2))},1032,["onResize"]);if(i||a.src){const v=this.src||a.src;return ae("img",{...a,loading:ue&&!this.intersectionObserverOptions&&s?"lazy":"eager",src:s&&this.intersectionObserverOptions?this.shouldStartLoading?v:void 0:v,"data-image-src":v,onLoad:this.mergedOnLoad,onError:this.mergedOnError,style:[a.style||"",{objectFit:this.objectFit},f?{height:"0",width:"0",visibility:"hidden",position:"absolute"}:""]})}}),R(),P("span",{ref:"selfRef",class:A([`${m}-avatar`,this.themeClass]),style:W(this.cssVars)},[j(()=>t),j(()=>s&&f)],6)}});export{ye as A};
