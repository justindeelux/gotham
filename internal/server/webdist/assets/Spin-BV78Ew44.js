import{b7 as v,b8 as o,b_ as $,bY as g,d as w,hU as B,b9 as k,o as d,c as u,bb as p,bI as i,a as z,_ as f,p as R,fC as T,e as V,bO as N,bc as P,c2 as _,bR as I,f as y,j as O,hV as W,bf as j,be as E}from"./index-Dn0GHF-w.js";import{u as L}from"./use-compitable-BMf0M5uu.js";var D=v([v("@keyframes spin-rotate",`
 from {
 transform: rotate(0);
 }
 to {
 transform: rotate(360deg);
 }
 `),o("spin-container",`
 position: relative;
 `,[o("spin-body",`
 position: absolute;
 top: 50%;
 left: 50%;
 transform: translateX(-50%) translateY(-50%);
 `,[$()])]),o("spin-body",`
 display: inline-flex;
 align-items: center;
 justify-content: center;
 flex-direction: column;
 `),o("spin",`
 display: inline-flex;
 height: var(--n-size);
 width: var(--n-size);
 font-size: var(--n-size);
 color: var(--n-color);
 `,[g("rotate",`
 animation: spin-rotate 2s linear infinite;
 `)]),o("spin-description",`
 display: inline-block;
 font-size: var(--n-font-size);
 color: var(--n-text-color);
 transition: color .3s var(--n-bezier);
 margin-top: 8px;
 `),o("spin-content",`
 opacity: 1;
 transition: opacity .3s var(--n-bezier);
 pointer-events: all;
 `,[g("spinning",`
 user-select: none;
 -webkit-user-select: none;
 pointer-events: none;
 opacity: var(--n-opacity-spinning);
 `)])]);const H={small:20,medium:18,large:16},K={...k.props,contentClass:String,contentStyle:[Object,String],description:String,size:{type:[String,Number],default:"medium"},show:{type:Boolean,default:!0},rotate:{type:Boolean,default:!0},spinning:{type:Boolean,validator:()=>!0,default:void 0},delay:Number,...B,strokeWidth:Number};var U=w({name:"Spin",props:K,slots:Object,setup(e){const{mergedClsPrefixRef:t,inlineThemeDisabled:a}=P(e),m=k("Spin","-spin",D,W,e,t),r=y(()=>{const{size:s}=e,{common:{cubicBezierEaseInOut:n},self:c}=m.value,{opacitySpinning:C,color:S,textColor:x}=c;return{"--n-bezier":n,"--n-opacity-spinning":C,"--n-size":typeof s=="number"?j(s):c[E("size",s)],"--n-color":S,"--n-text-color":x}}),l=a?_("spin",y(()=>{const{size:s}=e;return typeof s=="number"?String(s):s[0]}),r,e):void 0,b=L(e,["spinning","show"]),h=O(!1);return I(s=>{let n;if(b.value){const{delay:c}=e;if(c){n=window.setTimeout(()=>{h.value=!0},c),s(()=>{clearTimeout(n)});return}}h.value=b.value}),{mergedClsPrefix:t,active:h,mergedStrokeWidth:y(()=>{const{strokeWidth:s}=e;if(s!==void 0)return s;const{size:n}=e;return H[typeof n=="number"?"medium":n]}),cssVars:a?void 0:r,themeClass:l?.themeClass,onRender:l?.onRender}},render(){const{$slots:e,mergedClsPrefix:t,description:a}=this,m=e.icon&&this.rotate,r=(a||e.description)&&(d(),u("div",{class:i(`${t}-spin-description`)},[p(()=>a||e.description?.())],2)),l=e.icon?(d(),u("div",{key:1,class:i([`${t}-spin-body`,this.themeClass])},[z("div",{class:i([`${t}-spin`,m&&`${t}-spin--rotate`]),style:f(e.default?"":this.cssVars)},[p(()=>e.icon())],6),p(()=>r)],2)):(d(),u("div",{key:2,class:i([`${t}-spin-body`,this.themeClass])},[(d(),R(T,{clsPrefix:t,style:f(e.default?"":this.cssVars),stroke:this.stroke,"stroke-width":this.mergedStrokeWidth,radius:this.radius,scale:this.scale,class:i(`${t}-spin`)},null,8,["clsPrefix","style","stroke","stroke-width","radius","scale","class"])),p(()=>r)],2));return this.onRender?.(),e.default?(d(),u("div",{key:3,class:i([`${t}-spin-container`,this.themeClass]),style:f(this.cssVars)},[z("div",{class:i([`${t}-spin-content`,this.active&&`${t}-spin-content--spinning`,this.contentClass]),style:f(this.contentStyle)},[p(()=>e.default?.())],6),V(N,{name:"fade-in-transition"},{default:()=>this.active?l:null},1024)],6)):l}});export{U as S};
