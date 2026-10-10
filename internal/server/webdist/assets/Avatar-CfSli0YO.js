import{a0 as G,cy as D,a3 as A,dr as X,a2 as P,ds as Y,aV as T,d as q,a4 as I,cA as U,aD as J,o as R,c as j,a6 as w,an as $,ao as W,a7 as Q,Y as _,ax as Z,p as y,k as ee,aO as re,X as te,L as oe,g as O,x as se,da as ae,W as ne,ej as ie,a9 as le,ek as ce}from"./index-Cw_K-Arj.js";import{t as de}from"./Tag-CUeIbrDF.js";const ue=G&&"loading"in document.createElement("img");function fe(e={}){const{root:i=null}=e;return{hash:`${e.rootMargin||"0px 0px 0px 0px"}-${Array.isArray(e.threshold)?e.threshold.join(","):e.threshold??"0"}`,options:{...e,root:(typeof i=="string"?document.querySelector(i):i)||document.documentElement}}}const F=new WeakMap,C=new WeakMap,M=new WeakMap,he=(e,i,g)=>{if(!e)return()=>{};const s=fe(i),{root:c}=s.options;let a;const u=F.get(c);u?a=u:(a=new Map,F.set(c,a));let n,t;a.has(s.hash)?(t=a.get(s.hash),t[1].has(e)||(n=t[0],t[1].add(e),n.observe(e))):(n=new IntersectionObserver(v=>{v.forEach(b=>{if(b.isIntersecting){const p=C.get(b.target),z=M.get(b.target);p&&p(),z&&(z.value=!0)}})},s.options),n.observe(e),t=[n,new Set([e])],a.set(s.hash,t));let f=!1;const h=()=>{f||(C.delete(e),M.delete(e),f=!0,t[1].has(e)&&(t[0].unobserve(e),t[1].delete(e)),t[1].size<=0&&a.delete(s.hash),a.size||F.delete(c))};return C.set(e,h),M.set(e,g),h},ve=D("n-avatar-group");var me=A("avatar",`
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
`,[X(P("&","--n-merged-color: var(--n-color-modal);")),Y(P("&","--n-merged-color: var(--n-color-popover);")),P("img",`
 width: 100%;
 height: 100%;
 `),T("text",`
 white-space: nowrap;
 display: inline-block;
 position: absolute;
 left: 50%;
 top: 50%;
 `),A("icon",`
 vertical-align: bottom;
 font-size: calc(var(--n-merged-size) - 6px);
 `),T("text","line-height: 1.25")]);const ge=["src"],be={...I.props,size:[String,Number],src:String,circle:{type:Boolean,default:void 0},objectFit:String,round:{type:Boolean,default:void 0},bordered:{type:Boolean,default:void 0},onError:Function,fallbackSrc:String,intersectionObserverOptions:Object,lazy:Boolean,onLoad:Function,renderPlaceholder:Function,renderFallback:Function,imgProps:Object,color:String};var ye=q({name:"Avatar",props:be,slots:Object,setup(e){const{mergedClsPrefixRef:i,inlineThemeDisabled:g}=Q(e),s=y(!1);let c=null;const a=y(null),u=y(null),n=()=>{const{value:r}=a;if(r&&(c===null||c!==r.innerHTML)){c=r.innerHTML;const{value:o}=u;if(o){const{offsetWidth:d,offsetHeight:m}=o,{offsetWidth:l,offsetHeight:E}=r,x=.9,L=Math.min(d/l*x,m/E*x,1);r.style.transform=`translateX(-50%) translateY(-50%) scale(${L})`}}},t=_(ve,null),f=O(()=>{const{size:r}=e;if(r)return r;const{size:o}=t||{};return o||"medium"}),h=I("Avatar","-avatar",me,ie,e,i),v=_(de,null),b=O(()=>{if(t)return!0;const{round:r,circle:o}=e;return r!==void 0||o!==void 0?r||o:v?v.roundRef.value:!1}),p=O(()=>t?!0:e.bordered||!1),z=O(()=>{const r=f.value,o=b.value,d=p.value,{color:m}=e,{self:{borderRadius:l,fontSize:E,color:x,border:L,colorModal:V,colorPopover:N},common:{cubicBezierEaseInOut:K}}=h.value;let k;return typeof r=="number"?k=`${r}px`:k=h.value.self[le("height",r)],{"--n-font-size":E,"--n-border":d?L:"none","--n-border-radius":o?"50%":l,"--n-color":m||x,"--n-color-modal":m||V,"--n-color-popover":m||N,"--n-bezier":K,"--n-merged-size":`var(--n-avatar-size-override, ${k})`}}),B=g?Z("avatar",O(()=>{const r=f.value,o=b.value,d=p.value,{color:m}=e;let l="";return r&&(typeof r=="number"?l+=`a${r}`:l+=r[0]),o&&(l+="b"),d&&(l+="c"),m&&(l+=ce(m)),l}),z,e):void 0,S=y(!e.lazy);ee(()=>{if(e.lazy&&e.intersectionObserverOptions){let r;const o=re(()=>{r?.(),r=void 0,e.lazy&&(r=he(u.value,e.intersectionObserverOptions,S))});te(()=>{o(),r?.()})}}),oe(()=>e.src||e.imgProps?.src,()=>{s.value=!1});const H=y(!e.lazy);return{textRef:a,selfRef:u,mergedRoundRef:b,mergedClsPrefix:i,fitTextTransform:n,cssVars:g?void 0:z,themeClass:B?.themeClass,onRender:B?.onRender,hasLoadError:s,shouldStartLoading:S,loaded:H,mergedOnError:r=>{if(!S.value)return;s.value=!0;const{onError:o,imgProps:{onError:d}={}}=e;o?.(r),d?.(r)},mergedOnLoad:r=>{const{onLoad:o,imgProps:{onLoad:d}={}}=e;o?.(r),d?.(r),H.value=!0}}},render(){const{$slots:e,src:i,mergedClsPrefix:g,lazy:s,onRender:c,loaded:a,hasLoadError:u,imgProps:n={}}=this;c?.();let t;const f=!a&&!u&&(this.renderPlaceholder?this.renderPlaceholder():this.$slots.placeholder?.());return this.hasLoadError?t=this.renderFallback?this.renderFallback():U(e.fallback,()=>[(R(),j("img",{src:this.fallbackSrc,style:$({objectFit:this.objectFit})},null,12,ge))]):t=J(e.default,h=>{if(h)return R(),se(ae,{key:1,onResize:this.fitTextTransform},{default:()=>(R(),j("span",{ref:"textRef",class:W(`${g}-avatar__text`)},[w(()=>h)],2))},1032,["onResize"]);if(i||n.src){const v=this.src||n.src;return ne("img",{...n,loading:ue&&!this.intersectionObserverOptions&&s?"lazy":"eager",src:s&&this.intersectionObserverOptions?this.shouldStartLoading?v:void 0:v,"data-image-src":v,onLoad:this.mergedOnLoad,onError:this.mergedOnError,style:[n.style||"",{objectFit:this.objectFit},f?{height:"0",width:"0",visibility:"hidden",position:"absolute"}:""]})}}),R(),j("span",{ref:"selfRef",class:W([`${g}-avatar`,this.themeClass]),style:$(this.cssVars)},[w(()=>t),w(()=>s&&f)],6)}});export{ye as A};
