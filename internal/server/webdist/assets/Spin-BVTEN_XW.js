import{Z as g,_ as a,aT as $,as as b,d as w,ey as T,$ as k,o as d,c as u,a2 as p,al as i,a as z,ak as m,n as B,cU as R,e as V,aG as N,a3 as P,au as _,aL as W,z as y,s as E,ez as I,a6 as L,a5 as O}from"./index-BbmUl6Bq.js";import{u as j}from"./use-compitable-C4vPeImI.js";var D=g([g("@keyframes spin-rotate",`
 from {
 transform: rotate(0);
 }
 to {
 transform: rotate(360deg);
 }
 `),a("spin-container",`
 position: relative;
 `,[a("spin-body",`
 position: absolute;
 top: 50%;
 left: 50%;
 transform: translateX(-50%) translateY(-50%);
 `,[$()])]),a("spin-body",`
 display: inline-flex;
 align-items: center;
 justify-content: center;
 flex-direction: column;
 `),a("spin",`
 display: inline-flex;
 height: var(--n-size);
 width: var(--n-size);
 font-size: var(--n-size);
 color: var(--n-color);
 `,[b("rotate",`
 animation: spin-rotate 2s linear infinite;
 `)]),a("spin-description",`
 display: inline-block;
 font-size: var(--n-font-size);
 color: var(--n-text-color);
 transition: color .3s var(--n-bezier);
 margin-top: 8px;
 `),a("spin-content",`
 opacity: 1;
 transition: opacity .3s var(--n-bezier);
 pointer-events: all;
 `,[b("spinning",`
 user-select: none;
 -webkit-user-select: none;
 pointer-events: none;
 opacity: var(--n-opacity-spinning);
 `)])]);const H={small:20,medium:18,large:16},K={...k.props,contentClass:String,contentStyle:[Object,String],description:String,size:{type:[String,Number],default:"medium"},show:{type:Boolean,default:!0},rotate:{type:Boolean,default:!0},spinning:{type:Boolean,validator:()=>!0,default:void 0},delay:Number,...T,strokeWidth:Number};var U=w({name:"Spin",props:K,slots:Object,setup(e){const{mergedClsPrefixRef:s,inlineThemeDisabled:o}=P(e),f=k("Spin","-spin",D,I,e,s),r=y(()=>{const{size:t}=e,{common:{cubicBezierEaseInOut:n},self:c}=f.value,{opacitySpinning:S,color:C,textColor:x}=c;return{"--n-bezier":n,"--n-opacity-spinning":S,"--n-size":typeof t=="number"?L(t):c[O("size",t)],"--n-color":C,"--n-text-color":x}}),l=o?_("spin",y(()=>{const{size:t}=e;return typeof t=="number"?String(t):t[0]}),r,e):void 0,v=j(e,["spinning","show"]),h=E(!1);return W(t=>{let n;if(v.value){const{delay:c}=e;if(c){n=window.setTimeout(()=>{h.value=!0},c),t(()=>{clearTimeout(n)});return}}h.value=v.value}),{mergedClsPrefix:s,active:h,mergedStrokeWidth:y(()=>{const{strokeWidth:t}=e;if(t!==void 0)return t;const{size:n}=e;return H[typeof n=="number"?"medium":n]}),cssVars:o?void 0:r,themeClass:l?.themeClass,onRender:l?.onRender}},render(){const{$slots:e,mergedClsPrefix:s,description:o}=this,f=e.icon&&this.rotate,r=(o||e.description)&&(d(),u("div",{class:i(`${s}-spin-description`)},[p(()=>o||e.description?.())],2)),l=e.icon?(d(),u("div",{key:1,class:i([`${s}-spin-body`,this.themeClass])},[z("div",{class:i([`${s}-spin`,f&&`${s}-spin--rotate`]),style:m(e.default?"":this.cssVars)},[p(()=>e.icon())],6),p(()=>r)],2)):(d(),u("div",{key:2,class:i([`${s}-spin-body`,this.themeClass])},[(d(),B(R,{clsPrefix:s,style:m(e.default?"":this.cssVars),stroke:this.stroke,"stroke-width":this.mergedStrokeWidth,radius:this.radius,scale:this.scale,class:i(`${s}-spin`)},null,8,["clsPrefix","style","stroke","stroke-width","radius","scale","class"])),p(()=>r)],2));return this.onRender?.(),e.default?(d(),u("div",{key:3,class:i([`${s}-spin-container`,this.themeClass]),style:m(this.cssVars)},[z("div",{class:i([`${s}-spin-content`,this.active&&`${s}-spin-content--spinning`,this.contentClass]),style:m(this.contentStyle)},[p(()=>e.default?.())],6),V(N,{name:"fade-in-transition"},{default:()=>this.active?l:null},1024)],6)):l}});export{U as S};
