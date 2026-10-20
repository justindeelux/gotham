import{b3 as K,ej as X,b6 as T,eX as Y,b5 as j,eY as q,bX as _,d as D,b7 as I,em as U,ek as J,o as R,c as P,b9 as w,X as $,bG as A,ba as Q,aN as W,c0 as Z,j as y,n as ee,bP as re,M as te,W as oe,f as O,p as se,fB as ne,a_ as ae,gj as ie,bc as le,gk as ce}from"./index-BixTaOkY.js";import{t as de}from"./Tag-CUH657oN.js";const ue=K&&"loading"in document.createElement("img");function fe(e={}){const{root:i=null}=e;return{hash:`${e.rootMargin||"0px 0px 0px 0px"}-${Array.isArray(e.threshold)?e.threshold.join(","):e.threshold??"0"}`,options:{...e,root:(typeof i=="string"?document.querySelector(i):i)||document.documentElement}}}const F=new WeakMap,M=new WeakMap,B=new WeakMap,he=(e,i,b)=>{if(!e)return()=>{};const s=fe(i),{root:c}=s.options;let n;const u=F.get(c);u?n=u:(n=new Map,F.set(c,n));let a,t;n.has(s.hash)?(t=n.get(s.hash),t[1].has(e)||(a=t[0],t[1].add(e),a.observe(e))):(a=new IntersectionObserver(v=>{v.forEach(g=>{if(g.isIntersecting){const p=M.get(g.target),z=B.get(g.target);p&&p(),z&&(z.value=!0)}})},s.options),a.observe(e),t=[a,new Set([e])],n.set(s.hash,t));let f=!1;const h=()=>{f||(M.delete(e),B.delete(e),f=!0,t[1].has(e)&&(t[0].unobserve(e),t[1].delete(e)),t[1].size<=0&&n.delete(s.hash),n.size||F.delete(c))};return M.set(e,h),B.set(e,b),h},ve=X("n-avatar-group");var me=T("avatar",`
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
`,[Y(j("&","--n-merged-color: var(--n-color-modal);")),q(j("&","--n-merged-color: var(--n-color-popover);")),j("img",`
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
 `),_("text","line-height: 1.25")]);const be=["src"],ge={...I.props,size:[String,Number],src:String,circle:{type:Boolean,default:void 0},objectFit:String,round:{type:Boolean,default:void 0},bordered:{type:Boolean,default:void 0},onError:Function,fallbackSrc:String,intersectionObserverOptions:Object,lazy:Boolean,onLoad:Function,renderPlaceholder:Function,renderFallback:Function,imgProps:Object,color:String};var ye=D({name:"Avatar",props:ge,slots:Object,setup(e){const{mergedClsPrefixRef:i,inlineThemeDisabled:b}=Q(e),s=y(!1);let c=null;const n=y(null),u=y(null),a=()=>{const{value:r}=n;if(r&&(c===null||c!==r.innerHTML)){c=r.innerHTML;const{value:o}=u;if(o){const{offsetWidth:d,offsetHeight:m}=o,{offsetWidth:l,offsetHeight:E}=r,x=.9,L=Math.min(d/l*x,m/E*x,1);r.style.transform=`translateX(-50%) translateY(-50%) scale(${L})`}}},t=W(ve,null),f=O(()=>{const{size:r}=e;if(r)return r;const{size:o}=t||{};return o||"medium"}),h=I("Avatar","-avatar",me,ie,e,i),v=W(de,null),g=O(()=>{if(t)return!0;const{round:r,circle:o}=e;return r!==void 0||o!==void 0?r||o:v?v.roundRef.value:!1}),p=O(()=>t?!0:e.bordered||!1),z=O(()=>{const r=f.value,o=g.value,d=p.value,{color:m}=e,{self:{borderRadius:l,fontSize:E,color:x,border:L,colorModal:N,colorPopover:V},common:{cubicBezierEaseInOut:G}}=h.value;let k;return typeof r=="number"?k=`${r}px`:k=h.value.self[le("height",r)],{"--n-font-size":E,"--n-border":d?L:"none","--n-border-radius":o?"50%":l,"--n-color":m||x,"--n-color-modal":m||N,"--n-color-popover":m||V,"--n-bezier":G,"--n-merged-size":`var(--n-avatar-size-override, ${k})`}}),C=b?Z("avatar",O(()=>{const r=f.value,o=g.value,d=p.value,{color:m}=e;let l="";return r&&(typeof r=="number"?l+=`a${r}`:l+=r[0]),o&&(l+="b"),d&&(l+="c"),m&&(l+=ce(m)),l}),z,e):void 0,S=y(!e.lazy);ee(()=>{if(e.lazy&&e.intersectionObserverOptions){let r;const o=re(()=>{r?.(),r=void 0,e.lazy&&(r=he(u.value,e.intersectionObserverOptions,S))});te(()=>{o(),r?.()})}}),oe(()=>e.src||e.imgProps?.src,()=>{s.value=!1});const H=y(!e.lazy);return{textRef:n,selfRef:u,mergedRoundRef:g,mergedClsPrefix:i,fitTextTransform:a,cssVars:b?void 0:z,themeClass:C?.themeClass,onRender:C?.onRender,hasLoadError:s,shouldStartLoading:S,loaded:H,mergedOnError:r=>{if(!S.value)return;s.value=!0;const{onError:o,imgProps:{onError:d}={}}=e;o?.(r),d?.(r)},mergedOnLoad:r=>{const{onLoad:o,imgProps:{onLoad:d}={}}=e;o?.(r),d?.(r),H.value=!0}}},render(){const{$slots:e,src:i,mergedClsPrefix:b,lazy:s,onRender:c,loaded:n,hasLoadError:u,imgProps:a={}}=this;c?.();let t;const f=!n&&!u&&(this.renderPlaceholder?this.renderPlaceholder():this.$slots.placeholder?.());return this.hasLoadError?t=this.renderFallback?this.renderFallback():U(e.fallback,()=>[(R(),P("img",{src:this.fallbackSrc,style:$({objectFit:this.objectFit})},null,12,be))]):t=J(e.default,h=>{if(h)return R(),se(ne,{key:1,onResize:this.fitTextTransform},{default:()=>(R(),P("span",{ref:"textRef",class:A(`${b}-avatar__text`)},[w(()=>h)],2))},1032,["onResize"]);if(i||a.src){const v=this.src||a.src;return ae("img",{...a,loading:ue&&!this.intersectionObserverOptions&&s?"lazy":"eager",src:s&&this.intersectionObserverOptions?this.shouldStartLoading?v:void 0:v,"data-image-src":v,onLoad:this.mergedOnLoad,onError:this.mergedOnError,style:[a.style||"",{objectFit:this.objectFit},f?{height:"0",width:"0",visibility:"hidden",position:"absolute"}:""]})}}),R(),P("span",{ref:"selfRef",class:A([`${b}-avatar`,this.themeClass]),style:$(this.cssVars)},[w(()=>t),w(()=>s&&f)],6)}});export{ye as A};
