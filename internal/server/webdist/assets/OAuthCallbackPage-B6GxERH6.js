import{u as P,S as R,t as B}from"./text-B2PE0c6T.js";import{y as b,z as c,A as N,D as S,d as x,E as V,G as w,o as d,c as h,H as m,I as r,b as z,J as y,k as O,L as E,f as g,T as I,K as L,M as W,a as v,N as j,s as A,O as D,P as H,Q as K,g as M,h as q,w as _,u as k,j as F,l as G}from"./index-CPuOTAan.js";var J=b([b("@keyframes spin-rotate",`
 from {
 transform: rotate(0);
 }
 to {
 transform: rotate(360deg);
 }
 `),c("spin-container",`
 position: relative;
 `,[c("spin-body",`
 position: absolute;
 top: 50%;
 left: 50%;
 transform: translateX(-50%) translateY(-50%);
 `,[N()])]),c("spin-body",`
 display: inline-flex;
 align-items: center;
 justify-content: center;
 flex-direction: column;
 `),c("spin",`
 display: inline-flex;
 height: var(--n-size);
 width: var(--n-size);
 font-size: var(--n-size);
 color: var(--n-color);
 `,[S("rotate",`
 animation: spin-rotate 2s linear infinite;
 `)]),c("spin-description",`
 display: inline-block;
 font-size: var(--n-font-size);
 color: var(--n-text-color);
 transition: color .3s var(--n-bezier);
 margin-top: 8px;
 `),c("spin-content",`
 opacity: 1;
 transition: opacity .3s var(--n-bezier);
 pointer-events: all;
 `,[S("spinning",`
 user-select: none;
 -webkit-user-select: none;
 pointer-events: none;
 opacity: var(--n-opacity-spinning);
 `)])]);const Q={small:20,medium:18,large:16},U={...w.props,contentClass:String,contentStyle:[Object,String],description:String,size:{type:[String,Number],default:"medium"},show:{type:Boolean,default:!0},rotate:{type:Boolean,default:!0},spinning:{type:Boolean,validator:()=>!0,default:void 0},delay:Number,...V,strokeWidth:Number};var X=x({name:"Spin",props:U,slots:Object,setup(e){const{mergedClsPrefixRef:t,inlineThemeDisabled:a}=L(e),u=w("Spin","-spin",J,D,e,t),s=v(()=>{const{size:n}=e,{common:{cubicBezierEaseInOut:o},self:f}=u.value,{opacitySpinning:C,color:T,textColor:$}=f;return{"--n-bezier":o,"--n-opacity-spinning":C,"--n-size":typeof n=="number"?H(n):f[K("size",n)],"--n-color":T,"--n-text-color":$}}),i=a?W("spin",v(()=>{const{size:n}=e;return typeof n=="number"?String(n):n[0]}),s,e):void 0,p=P(e,["spinning","show"]),l=A(!1);return j(n=>{let o;if(p.value){const{delay:f}=e;if(f){o=window.setTimeout(()=>{l.value=!0},f),n(()=>{clearTimeout(o)});return}}l.value=p.value}),{mergedClsPrefix:t,active:l,mergedStrokeWidth:v(()=>{const{strokeWidth:n}=e;if(n!==void 0)return n;const{size:o}=e;return Q[typeof o=="number"?"medium":o]}),cssVars:a?void 0:s,themeClass:i?.themeClass,onRender:i?.onRender}},render(){const{$slots:e,mergedClsPrefix:t,description:a}=this,u=e.icon&&this.rotate,s=(a||e.description)&&(d(),h("div",{class:r(`${t}-spin-description`)},[m(()=>a||e.description?.())],2)),i=e.icon?(d(),h("div",{key:1,class:r([`${t}-spin-body`,this.themeClass])},[z("div",{class:r([`${t}-spin`,u&&`${t}-spin--rotate`]),style:y(e.default?"":this.cssVars)},[m(()=>e.icon())],6),m(()=>s)],2)):(d(),h("div",{key:2,class:r([`${t}-spin-body`,this.themeClass])},[(d(),O(E,{clsPrefix:t,style:y(e.default?"":this.cssVars),stroke:this.stroke,"stroke-width":this.mergedStrokeWidth,radius:this.radius,scale:this.scale,class:r(`${t}-spin`)},null,8,["clsPrefix","style","stroke","stroke-width","radius","scale","class"])),m(()=>s)],2));return this.onRender?.(),e.default?(d(),h("div",{key:3,class:r([`${t}-spin-container`,this.themeClass]),style:y(this.cssVars)},[z("div",{class:r([`${t}-spin-content`,this.active&&`${t}-spin-content--spinning`,this.contentClass]),style:y(this.contentStyle)},[m(()=>e.default?.())],6),g(I,{name:"fade-in-transition"},{default:()=>this.active?i:null},1024)],6)):i}});const Y={class:"auth-page"},te=x({__name:"OAuthCallbackPage",setup(e){const t=M(),a=F();function u(){const s=window.location.hash.replace(/^#/,"");if(!s)return null;const i=new URLSearchParams(s),p=i.get("access_token"),l=i.get("refresh_token");return!p||!l?null:{access_token:p,refresh_token:l}}return q(async()=>{const s=u();if(!s){await a.replace({path:"/login",query:{error:"oauth_failed"}});return}t.setSession(s),window.history.replaceState(null,"","/oauth/callback"),await a.replace({path:"/dashboard"})}),(s,i)=>(d(),h("div",Y,[g(k(R),{vertical:"",align:"center",size:16},{default:_(()=>[g(k(X),{size:"large"}),g(k(B),{depth:"3"},{default:_(()=>[...i[0]||(i[0]=[G("Signing you in…",-1)])]),_:1})]),_:1})]))}});export{te as default};
